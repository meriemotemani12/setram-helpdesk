package seed_test

import (
	"context"
	"testing"
	"time"

	"github.com/meriem/setram-helpdesk/internal/auth"
	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/seed"
	"github.com/meriem/setram-helpdesk/internal/service"
	"github.com/meriem/setram-helpdesk/internal/store/memory"
)

func TestDemoSeed(t *testing.T) {
	auth.PBKDF2Iterations = 1000
	tm, _ := auth.NewTokenManager("0123456789abcdef0123456789abcdef", time.Hour)
	svc, err := service.New(memory.New(), tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.EnsureAdmin(ctx, "admin@setram.dz", "motdepasse-admin", "Admin"); err != nil {
		t.Fatal(err)
	}
	admin := service.Actor{ID: 1, Role: domain.RoleAdmin}

	before := svc.Now
	if err := seed.Demo(ctx, svc, admin, "demo-password"); err != nil {
		t.Fatal(err)
	}
	if svc.Now().Sub(before()) > time.Minute {
		t.Fatal("l'horloge du service doit être restaurée après le seed")
	}

	st, err := svc.Stats(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalTickets != 11 || st.ResolvedCount != 7 {
		t.Errorf("tickets : total=%d résolus=%d", st.TotalTickets, st.ResolvedCount)
	}
	if st.AvgResolutionHours <= 0 || st.AvgFirstResponseHours <= 0 {
		t.Errorf("durées moyennes nulles : %+v", st)
	}
	if st.OverdueMaintenance != 1 {
		t.Errorf("maintenances en retard : %d", st.OverdueMaintenance)
	}
	if len(st.TopEquipment) == 0 || st.TopEquipment[0].AssetTag != "IMP-001" && st.TopEquipment[0].AssetTag != "GUI-001" {
		t.Errorf("top équipements : %+v", st.TopEquipment)
	}
	eq, _ := svc.ListEquipment(ctx, service.EquipmentFilter{})
	if len(eq) != 6 {
		t.Errorf("équipements : %d", len(eq))
	}

	// Idempotent
	if err := seed.Demo(ctx, svc, admin, "demo-password"); err != nil {
		t.Fatal(err)
	}
	st2, _ := svc.Stats(ctx, admin)
	if st2.TotalTickets != 11 {
		t.Errorf("le second appel ne doit rien ajouter : %d", st2.TotalTickets)
	}
	// Les comptes de démo peuvent se connecter
	if _, err := svc.Login(ctx, "technicien1@setram.test", "demo-password"); err != nil {
		t.Errorf("connexion démo : %v", err)
	}
}
