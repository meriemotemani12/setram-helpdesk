// Package config lit la configuration depuis les variables d'environnement.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr        string
	Store       string // "postgres" ou "memory"
	DatabaseURL string
	AutoMigrate bool

	JWTSecret string
	TokenTTL  time.Duration

	AdminEmail    string
	AdminPassword string
	AdminName     string

	AnthropicAPIKey string
	AnthropicModel  string

	GroqAPIKey string
	GroqModel  string

	SeedDemo     bool
	DemoPassword string
}

// Load construit la configuration. getenv est injectable pour les tests.
func Load(getenv func(string) string) (*Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := &Config{
		Addr:            get("ADDR", ":8080"),
		Store:           strings.ToLower(get("STORE", "postgres")),
		DatabaseURL:     get("DATABASE_URL", ""),
		JWTSecret:       get("JWT_SECRET", ""),
		AdminEmail:      get("ADMIN_EMAIL", ""),
		AdminPassword:   getenv("ADMIN_PASSWORD"),
		AdminName:       get("ADMIN_NAME", "Administrateur"),
		AnthropicAPIKey: get("ANTHROPIC_API_KEY", ""),
		AnthropicModel:  get("ANTHROPIC_MODEL", "claude-haiku-5-5"),
		DemoPassword:    get("DEMO_PASSWORD", ""),
		GroqAPIKey:      get("GROQ_API_KEY", ""),
		GroqModel:       get("GROQ_MODEL", "openai/gpt-oss-20b"),
	}

	var err error
	if c.AutoMigrate, err = boolVar(get("AUTO_MIGRATE", "true")); err != nil {
		return nil, fmt.Errorf("AUTO_MIGRATE : %w", err)
	}
	if c.SeedDemo, err = boolVar(get("SEED_DEMO", "false")); err != nil {
		return nil, fmt.Errorf("SEED_DEMO : %w", err)
	}
	if c.TokenTTL, err = time.ParseDuration(get("TOKEN_TTL", "12h")); err != nil || c.TokenTTL <= 0 {
		return nil, errors.New("TOKEN_TTL : durée invalide (exemple : 12h)")
	}

	switch c.Store {
	case "postgres":
		if c.DatabaseURL == "" {
			return nil, errors.New("DATABASE_URL est obligatoire avec STORE=postgres")
		}
	case "memory":
	default:
		return nil, fmt.Errorf("STORE=%q inconnu (postgres ou memory)", c.Store)
	}
	if len(c.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET est obligatoire et doit contenir au moins 32 caractères")
	}
	if (c.AdminEmail == "") != (c.AdminPassword == "") {
		return nil, errors.New("ADMIN_EMAIL et ADMIN_PASSWORD doivent être définis ensemble")
	}
	if c.SeedDemo && c.DemoPassword == "" {
		return nil, errors.New("SEED_DEMO=true exige DEMO_PASSWORD (mot de passe des comptes de démonstration, 8 caractères minimum)")
	}
	return c, nil
}

func boolVar(s string) (bool, error) {
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, errors.New("valeur booléenne attendue (true/false)")
	}
	return b, nil
}
