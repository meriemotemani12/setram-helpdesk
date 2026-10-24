// Commande server : API REST + interface web du helpdesk SETRAM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/meriem/setram-helpdesk/internal/ai"
	"github.com/meriem/setram-helpdesk/internal/auth"
	"github.com/meriem/setram-helpdesk/internal/config"
	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/httpapi"
	"github.com/meriem/setram-helpdesk/internal/seed"
	"github.com/meriem/setram-helpdesk/internal/service"
	"github.com/meriem/setram-helpdesk/internal/store/memory"
	"github.com/meriem/setram-helpdesk/internal/store/postgres"
	"github.com/meriem/setram-helpdesk/migrations"
	"github.com/meriem/setram-helpdesk/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("arrêt sur erreur", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Persistance ---
	var st service.Store
	switch cfg.Store {
	case "memory":
		log.Warn("STORE=memory : les données sont perdues à l'arrêt (développement uniquement)")
		st = memory.New()
	default:
		pg, err := postgres.New(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pg.Close()
		if cfg.AutoMigrate {
			if err := pg.Migrate(ctx, migrations.FS, log); err != nil {
				return err
			}
		}
		st = pg
	}

	// --- Services ---
	tokens, err := auth.NewTokenManager(cfg.JWTSecret, cfg.TokenTTL)
	if err != nil {
		return err
	}
	var classifier ai.Classifier = ai.KeywordClassifier{}

	switch {
	case cfg.GroqAPIKey != "":
		log.Info("classification Groq activée", "model", cfg.GroqModel)
		classifier = ai.FallbackClassifier{
			Primary: &ai.GroqClassifier{
				APIKey: cfg.GroqAPIKey,
				Model:  cfg.GroqModel,
			},
			Fallback: ai.KeywordClassifier{},
		}

	case cfg.AnthropicAPIKey != "":
		log.Info("classification Anthropic activée", "model", cfg.AnthropicModel)
		classifier = ai.FallbackClassifier{
			Primary: &ai.AnthropicClassifier{
				APIKey: cfg.AnthropicAPIKey,
				Model:  cfg.AnthropicModel,
			},
			Fallback: ai.KeywordClassifier{},
		}

	default:
		log.Info("Aucune clé IA configurée : classification par règles locales")
	}

	svc, err := service.New(st, tokens, classifier)
	if err != nil {
		return err
	}

	// --- Premier administrateur et données de démonstration ---
	if cfg.AdminEmail != "" {
		created, err := svc.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword, cfg.AdminName)
		if err != nil {
			return err
		}
		if created {
			log.Info("administrateur créé", "email", cfg.AdminEmail)
		}
	}
	if cfg.SeedDemo {
		admin, err := st.GetUserByEmail(ctx, cfg.AdminEmail)
		if err != nil {
			return errors.New("SEED_DEMO exige ADMIN_EMAIL/ADMIN_PASSWORD (compte administrateur existant)")
		}
		log.Warn("données de démonstration : comptes *@setram.test avec DEMO_PASSWORD (développement uniquement)")
		if err := seed.Demo(ctx, svc, service.Actor{ID: admin.ID, Role: domain.RoleAdmin}, cfg.DemoPassword); err != nil {
			return err
		}
	}

	// --- Serveur HTTP ---
	static, err := web.Static()
	if err != nil {
		return err
	}
	api := httpapi.New(svc, httpapi.Options{Static: static, Logger: log})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("serveur démarré", "addr", cfg.Addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("arrêt demandé")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
