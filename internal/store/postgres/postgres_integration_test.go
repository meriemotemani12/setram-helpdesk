//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/store/postgres"
	"github.com/meriem/setram-helpdesk/migrations"
)

// newStore ouvre la base de test. Le schéma public est réinitialisé : on refuse
// donc toute base dont le nom ne contient pas « test ».
func newStore(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL non défini")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(strings.ToLower(strings.TrimPrefix(u.Path, "/")), "test") {
		t.Fatalf("TEST_DATABASE_URL doit cibler une base dont le nom contient « test »")
	}
	ctx := context.Background()
	st, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if _, err := st.Pool().Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Deux passages : la seconde exécution doit être sans effet (idempotence).
	for i := 0; i < 2; i++ {
		if err := st.Migrate(ctx, migrations.FS, log); err != nil {
			t.Fatalf("migration (passage %d) : %v", i+1, err)
		}
	}
	return st
}

func mustUser(t *testing.T, st *postgres.Store, email string, role domain.Role) *domain.User {
	t.Helper()
	u := &domain.User{Email: email, FullName: "Test " + email, Role: role, PasswordHash: "hash", Active: true, CreatedAt: time.Now()}
	if err := st.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUsersAndEquipmentConstraints(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	mustUser(t, st, "Ali@Exemple.dz", domain.RoleEmployee)
	dup := &domain.User{Email: "ali@exemple.dz", FullName: "Doublon", Role: domain.RoleEmployee, PasswordHash: "h", Active: true, CreatedAt: time.Now()}
	if err := st.CreateUser(ctx, dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("e-mail en double (casse différente) : attendu ErrConflict, obtenu %v", err)
	}
	if u, err := st.GetUserByEmail(ctx, "ALI@EXEMPLE.DZ"); err != nil || u.Email != "Ali@Exemple.dz" {
		t.Fatalf("recherche insensible à la casse : %v %v", u, err)
	}

	now := time.Now()
	e := &domain.Equipment{AssetTag: "PC-1", Name: "Poste", Type: domain.EquipComputer, Status: domain.EquipActive, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateEquipment(ctx, e); err != nil {
		t.Fatal(err)
	}
	e2 := *e
	e2.ID = 0
	if err := st.CreateEquipment(ctx, &e2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("numéro d'inventaire en double : attendu ErrConflict, obtenu %v", err)
	}
}

func TestTicketLifecycleAndStats(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	emp := mustUser(t, st, "emp@exemple.dz", domain.RoleEmployee)
	tech := mustUser(t, st, "tech@exemple.dz", domain.RoleTechnician)

	created := time.Now().Add(-3 * time.Hour)
	tk := &domain.Ticket{Title: "Imprimante", Description: "bourrage", Category: domain.CategoryPrinter,
		Priority: domain.PriorityHigh, RequesterID: emp.ID, CreatedAt: created, UpdatedAt: created}
	if err := st.CreateTicket(ctx, tk); err != nil {
		t.Fatal(err)
	}

	got, err := st.TransitionTicket(ctx, tk.ID, domain.StatusInProgress, tech.ID, created.Add(time.Hour))
	if err != nil || got.Status != domain.StatusInProgress || got.AssigneeID == nil || *got.AssigneeID != tech.ID {
		t.Fatalf("new → in_progress : %+v %v", got, err)
	}
	if _, err := st.TransitionTicket(ctx, tk.ID, domain.StatusDone, tech.ID, created.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TransitionTicket(ctx, tk.ID, domain.StatusNew, tech.ID, time.Now()); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("done → new doit être refusé, obtenu %v", err)
	}

	hist, err := st.ListHistory(ctx, tk.ID)
	if err != nil || len(hist) != 3 || hist[0].FromStatus != nil {
		t.Fatalf("historique attendu : création + 2 transitions, obtenu %d (%v)", len(hist), err)
	}

	stats, err := st.Stats(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalTickets != 1 || stats.ResolvedCount != 1 {
		t.Fatalf("stats : %+v", stats)
	}
	if stats.AvgResolutionHours < 1.9 || stats.AvgResolutionHours > 2.1 {
		t.Fatalf("durée moyenne de résolution attendue ≈ 2 h, obtenu %v", stats.AvgResolutionHours)
	}
}

// Deux techniciens prennent le même ticket en même temps : une seule transition doit réussir.
func TestConcurrentTransitionOnlyOneWins(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	emp := mustUser(t, st, "emp@exemple.dz", domain.RoleEmployee)
	t1 := mustUser(t, st, "t1@exemple.dz", domain.RoleTechnician)
	t2 := mustUser(t, st, "t2@exemple.dz", domain.RoleTechnician)

	now := time.Now()
	tk := &domain.Ticket{Title: "Réseau", Description: "lent", Category: domain.CategoryNetwork,
		Priority: domain.PriorityMedium, RequesterID: emp.ID, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateTicket(ctx, tk); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, tech := range []*domain.User{t1, t2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = st.TransitionTicket(ctx, tk.ID, domain.StatusInProgress, tech.ID, time.Now())
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("erreur inattendue : %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("une seule transition doit réussir, %d ont réussi", wins)
	}
}
