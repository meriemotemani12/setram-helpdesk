package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusNew, StatusInProgress, true},
		{StatusNew, StatusDone, false},
		{StatusInProgress, StatusDone, true},
		{StatusInProgress, StatusNew, true},
		{StatusDone, StatusInProgress, true},
		{StatusDone, StatusNew, false},
		{StatusNew, StatusNew, false},
	}
	for _, c := range cases {
		if got := CanTransition(c.from, c.to); got != c.want {
			t.Errorf("CanTransition(%s,%s)=%v, attendu %v", c.from, c.to, got, c.want)
		}
	}
}

func TestApplyTransitionWorkflow(t *testing.T) {
	created := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	tk := &Ticket{Status: StatusNew, CreatedAt: created}

	// new → in_progress : assigne et renseigne started_at
	t1 := created.Add(30 * time.Minute)
	if err := ApplyTransition(tk, StatusInProgress, 7, t1); err != nil {
		t.Fatal(err)
	}
	if tk.AssigneeID == nil || *tk.AssigneeID != 7 {
		t.Fatalf("assigné attendu 7, obtenu %v", tk.AssigneeID)
	}
	if tk.StartedAt == nil || !tk.StartedAt.Equal(t1) {
		t.Fatalf("started_at attendu %v, obtenu %v", t1, tk.StartedAt)
	}

	// in_progress → done : resolved_at
	t2 := created.Add(3 * time.Hour)
	if err := ApplyTransition(tk, StatusDone, 7, t2); err != nil {
		t.Fatal(err)
	}
	d, ok := tk.ResolutionDuration()
	if !ok || d != 3*time.Hour {
		t.Fatalf("durée attendue 3h, obtenue %v (ok=%v)", d, ok)
	}

	// done → in_progress : réouverture, resolved_at effacé, started_at conservé
	t3 := created.Add(4 * time.Hour)
	if err := ApplyTransition(tk, StatusInProgress, 9, t3); err != nil {
		t.Fatal(err)
	}
	if tk.ResolvedAt != nil {
		t.Fatal("resolved_at devrait être effacé après réouverture")
	}
	if !tk.StartedAt.Equal(t1) {
		t.Fatal("started_at ne doit pas changer à la réouverture")
	}
	if *tk.AssigneeID != 7 {
		t.Fatal("l'assigné initial doit être conservé à la réouverture")
	}

	// in_progress → new : l'assigné est retiré
	if err := ApplyTransition(tk, StatusNew, 7, t3.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if tk.AssigneeID != nil {
		t.Fatal("l'assigné devrait être retiré à la remise en file")
	}
}

func TestApplyTransitionInvalid(t *testing.T) {
	tk := &Ticket{Status: StatusNew}
	err := ApplyTransition(tk, StatusDone, 1, time.Now())
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("ErrInvalidTransition attendu, obtenu %v", err)
	}
	if tk.Status != StatusNew {
		t.Fatal("le statut ne doit pas changer en cas d'erreur")
	}
	var ve *ValidationError
	if err := ApplyTransition(tk, Status("bogus"), 1, time.Now()); !errors.As(err, &ve) {
		t.Fatalf("ValidationError attendue, obtenu %v", err)
	}
}

func TestMaintenanceOverdue(t *testing.T) {
	now := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	if !(&Equipment{Status: EquipActive, NextMaintenanceAt: &past}).MaintenanceOverdue(now) {
		t.Error("devrait être en retard")
	}
	if (&Equipment{Status: EquipActive, NextMaintenanceAt: &future}).MaintenanceOverdue(now) {
		t.Error("ne devrait pas être en retard")
	}
	if (&Equipment{Status: EquipRetired, NextMaintenanceAt: &past}).MaintenanceOverdue(now) {
		t.Error("un équipement retiré n'est jamais en retard")
	}
	if (&Equipment{Status: EquipActive}).MaintenanceOverdue(now) {
		t.Error("sans date prévue, pas de retard")
	}
}

func TestComputeStats(t *testing.T) {
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	at := func(h float64) *time.Time { v := base.Add(time.Duration(h * float64(time.Hour))); return &v }
	eq1, eq2 := int64(1), int64(2)
	tickets := []*Ticket{
		{ID: 1, Status: StatusDone, Category: CategoryPrinter, CreatedAt: base, StartedAt: at(1), ResolvedAt: at(4), EquipmentID: &eq1},
		{ID: 2, Status: StatusDone, Category: CategoryPrinter, CreatedAt: base, StartedAt: at(3), ResolvedAt: at(8), EquipmentID: &eq1},
		{ID: 3, Status: StatusInProgress, Category: CategoryNetwork, CreatedAt: base, StartedAt: at(2), EquipmentID: &eq2},
		{ID: 4, Status: StatusNew, Category: CategoryComputer, CreatedAt: base},
		{ID: 5, Status: StatusNew, Category: CategoryNetwork, CreatedAt: base},
	}
	now := base.Add(48 * time.Hour)
	past := base.Add(-time.Hour)
	equipment := []*Equipment{
		{ID: 1, AssetTag: "IMP-001", Name: "Imprimante dépôt", Status: EquipActive, NextMaintenanceAt: &past},
		{ID: 2, AssetTag: "SW-001", Name: "Switch", Status: EquipActive},
	}
	st := ComputeStats(tickets, equipment, now)

	if st.TotalTickets != 5 || st.ResolvedCount != 2 {
		t.Fatalf("totaux : %+v", st)
	}
	// (4h + 8h) / 2 = 6h ; premières réponses (1h, 3h, 2h) → 2h
	if st.AvgResolutionHours != 6 {
		t.Errorf("durée moyenne de résolution %v, attendu 6", st.AvgResolutionHours)
	}
	if st.AvgFirstResponseHours != 2 {
		t.Errorf("délai moyen de prise en charge %v, attendu 2", st.AvgFirstResponseHours)
	}
	if st.ByStatus[0] != (StatusCount{StatusNew, 2}) || st.ByStatus[1] != (StatusCount{StatusInProgress, 1}) || st.ByStatus[2] != (StatusCount{StatusDone, 2}) {
		t.Errorf("par statut : %+v", st.ByStatus)
	}
	// imprimante 2, réseau 2 (égalité → ordre alphabétique), ordinateur 1
	if st.ByCategory[0] != (CategoryCount{CategoryNetwork, 2}) || st.ByCategory[1] != (CategoryCount{CategoryPrinter, 2}) || st.ByCategory[2] != (CategoryCount{CategoryComputer, 1}) {
		t.Errorf("par catégorie : %+v", st.ByCategory)
	}
	if len(st.TopEquipment) != 2 || st.TopEquipment[0].AssetTag != "IMP-001" || st.TopEquipment[0].Count != 2 {
		t.Errorf("top équipements : %+v", st.TopEquipment)
	}
	if st.OverdueMaintenance != 1 {
		t.Errorf("maintenances en retard %d, attendu 1", st.OverdueMaintenance)
	}

	empty := ComputeStats(nil, nil, now)
	if empty.TotalTickets != 0 || len(empty.ByStatus) != 3 || empty.ByCategory == nil || empty.TopEquipment == nil {
		t.Errorf("statistiques vides : %+v", empty)
	}
}
