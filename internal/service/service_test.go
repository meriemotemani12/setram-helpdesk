package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meriem/setram-helpdesk/internal/ai"
	"github.com/meriem/setram-helpdesk/internal/auth"
	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/service"
	"github.com/meriem/setram-helpdesk/internal/store/memory"
)

type env struct {
	svc                     *service.Service
	admin, tech, emp1, emp2 service.Actor
	clock                   *time.Time
}

type brokenClassifier struct{}

func (brokenClassifier) Classify(context.Context, string, string) (*ai.Suggestion, error) {
	return nil, errors.New("IA indisponible")
}

func newEnv(t *testing.T, cl ai.Classifier) *env {
	t.Helper()
	auth.PBKDF2Iterations = 1000 // tests rapides ; la production garde la valeur OWASP
	tm, err := auth.NewTokenManager("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(memory.New(), tm, cl)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return clock }
	ctx := context.Background()

	created, err := svc.EnsureAdmin(ctx, "Admin@Setram.dz", "motdepasse-admin", "Administrateur")
	if err != nil || !created {
		t.Fatalf("EnsureAdmin: %v, %v", created, err)
	}
	adminUser, err := svc.Me(ctx, service.Actor{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	admin := service.Actor{ID: adminUser.ID, Role: domain.RoleAdmin}

	mk := func(email, name string, role domain.Role) service.Actor {
		u, err := svc.CreateUser(ctx, admin, service.UserInput{Email: email, FullName: name, Role: role, Password: "motdepasse-ok"})
		if err != nil {
			t.Fatalf("CreateUser %s: %v", email, err)
		}
		return service.Actor{ID: u.ID, Role: u.Role}
	}
	e := &env{svc: svc, admin: admin, clock: &clock}
	e.tech = mk("tech@setram.dz", "Technicien Un", domain.RoleTechnician)
	e.emp1 = mk("emp1@setram.dz", "Employé Un", domain.RoleEmployee)
	e.emp2 = mk("emp2@setram.dz", "Employé Deux", domain.RoleEmployee)
	// l'horloge du service lit la variable partagée
	svc.Now = func() time.Time { return *e.clock }
	return e
}

func (e *env) advance(d time.Duration) { *e.clock = e.clock.Add(d) }

func TestLoginAndAuthenticate(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()

	res, err := e.svc.Login(ctx, "  EMP1@setram.dz ", "motdepasse-ok")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Role != domain.RoleEmployee {
		t.Fatalf("rôle %s", res.User.Role)
	}
	if b, _ := marshalHidesHash(res.User); b {
		t.Fatal("le hash du mot de passe ne doit jamais être sérialisé")
	}
	a, err := e.svc.Authenticate(ctx, res.Token)
	if err != nil || a.ID != e.emp1.ID || a.Role != domain.RoleEmployee {
		t.Fatalf("Authenticate: %+v, %v", a, err)
	}

	for _, c := range [][2]string{{"emp1@setram.dz", "mauvais"}, {"inconnu@setram.dz", "motdepasse-ok"}, {"", ""}} {
		if _, err := e.svc.Login(ctx, c[0], c[1]); !errors.Is(err, domain.ErrUnauthorized) {
			t.Errorf("Login(%q) : ErrUnauthorized attendu, obtenu %v", c[0], err)
		}
	}
	if _, err := e.svc.Authenticate(ctx, "n.importe.quoi"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("jeton invalide : %v", err)
	}
}

func TestEnsureAdminOnlyOnce(t *testing.T) {
	e := newEnv(t, nil)
	created, err := e.svc.EnsureAdmin(context.Background(), "autre@setram.dz", "motdepasse-admin", "Autre")
	if err != nil || created {
		t.Fatalf("aucun admin ne doit être créé si des utilisateurs existent : %v, %v", created, err)
	}
}

func TestCreateUserRules(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	ok := service.UserInput{Email: "nouveau@setram.dz", FullName: "Nouveau", Role: domain.RoleEmployee, Password: "motdepasse-ok"}

	if _, err := e.svc.CreateUser(ctx, e.tech, ok); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("un technicien ne crée pas d'utilisateur : %v", err)
	}
	if _, err := e.svc.CreateUser(ctx, e.admin, ok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateUser(ctx, e.admin, ok); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("doublon d'e-mail : %v", err)
	}
	bad := []service.UserInput{
		{Email: "pas-un-mail", FullName: "X Y", Role: domain.RoleEmployee, Password: "motdepasse-ok"},
		{Email: "a@setram.dz", FullName: "X", Role: domain.RoleEmployee, Password: "motdepasse-ok"},
		{Email: "b@setram.dz", FullName: "Xy", Role: "root", Password: "motdepasse-ok"},
		{Email: "c@setram.dz", FullName: "Xy", Role: domain.RoleEmployee, Password: "court"},
	}
	for i, in := range bad {
		var ve *domain.ValidationError
		if _, err := e.svc.CreateUser(ctx, e.admin, in); !errors.As(err, &ve) {
			t.Errorf("cas %d : ValidationError attendue, obtenu %v", i, err)
		}
	}
}

func TestTicketWorkflowAndStats(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()

	eq, err := e.svc.CreateEquipment(ctx, e.tech, service.EquipmentInput{
		AssetTag: "GUI-001", Name: "Distributeur Gare Sud", Type: domain.EquipTicketMachine, Location: "Gare Sud",
	})
	if err != nil {
		t.Fatal(err)
	}

	// L'employé déclare une panne ; l'IA (règles locales) suggère catégorie et priorité.
	tk, err := e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{
		Title: "Guichet bloqué", Description: "Le distributeur de billets ne fonctionne pas", EquipmentID: &eq.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != domain.StatusNew || tk.RequesterID != e.emp1.ID {
		t.Fatalf("ticket initial inattendu : %+v", tk)
	}
	if tk.Category != domain.CategoryTicketing || tk.Priority != domain.PriorityCritical {
		t.Errorf("classification : %s / %s", tk.Category, tk.Priority)
	}
	if tk.AISource == nil || *tk.AISource != ai.SourceKeywords {
		t.Errorf("source IA : %v", tk.AISource)
	}
	if tk.EquipmentTag != "GUI-001" || tk.RequesterName != "Employé Un" {
		t.Errorf("champs joints : %+v", tk)
	}

	// Un employé ne peut pas faire avancer le workflow.
	if _, err := e.svc.ChangeStatus(ctx, e.emp1, tk.ID, domain.StatusInProgress); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("employé : %v", err)
	}
	// Saut interdit new → done.
	if _, err := e.svc.ChangeStatus(ctx, e.tech, tk.ID, domain.StatusDone); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("new → done : %v", err)
	}

	e.advance(30 * time.Minute)
	tk, err = e.svc.ChangeStatus(ctx, e.tech, tk.ID, domain.StatusInProgress)
	if err != nil {
		t.Fatal(err)
	}
	if tk.AssigneeID == nil || *tk.AssigneeID != e.tech.ID || tk.AssigneeName != "Technicien Un" {
		t.Errorf("auto-assignation : %+v", tk)
	}
	e.advance(2 * time.Hour)
	tk, err = e.svc.ChangeStatus(ctx, e.tech, tk.ID, domain.StatusDone)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := tk.ResolutionDuration(); !ok || d != 150*time.Minute {
		t.Errorf("durée %v (ok=%v), attendu 2h30", d, ok)
	}

	if _, err := e.svc.AddComment(ctx, e.tech, tk.ID, "Carte mère remplacée"); err != nil {
		t.Fatal(err)
	}
	detail, err := e.svc.GetTicket(ctx, e.emp1, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.History) != 3 || detail.History[0].FromStatus != nil || detail.History[0].ToStatus != domain.StatusNew {
		t.Errorf("historique : %+v", detail.History)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].AuthorName != "Technicien Un" {
		t.Errorf("commentaires : %+v", detail.Comments)
	}

	if _, err := e.svc.Stats(ctx, e.emp1); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("stats employé : %v", err)
	}
	st, err := e.svc.Stats(ctx, e.tech)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalTickets != 1 || st.ResolvedCount != 1 || st.AvgResolutionHours != 2.5 || st.AvgFirstResponseHours != 0.5 {
		t.Errorf("stats : %+v", st)
	}
	if len(st.TopEquipment) != 1 || st.TopEquipment[0].AssetTag != "GUI-001" {
		t.Errorf("top équipements : %+v", st.TopEquipment)
	}
}

func TestEmployeeVisibilityIsolation(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	t1, _ := e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{Title: "Imprimante", Description: "bourrage papier récurrent"})
	if _, err := e.svc.CreateTicket(ctx, e.emp2, service.TicketInput{Title: "Réseau", Description: "plus de connexion wifi"}); err != nil {
		t.Fatal(err)
	}

	list, err := e.svc.ListTickets(ctx, e.emp1, service.TicketFilter{})
	if err != nil || len(list) != 1 || list[0].ID != t1.ID {
		t.Fatalf("emp1 ne doit voir que son ticket : %v, %v", list, err)
	}
	// Tentative de contourner le filtre : le service force RequesterID.
	other := e.emp2.ID
	list, _ = e.svc.ListTickets(ctx, e.emp1, service.TicketFilter{RequesterID: &other})
	if len(list) != 1 || list[0].RequesterID != e.emp1.ID {
		t.Fatalf("le filtre demandeur doit être imposé : %v", list)
	}
	if list, _ := e.svc.ListTickets(ctx, e.tech, service.TicketFilter{}); len(list) != 2 {
		t.Errorf("le technicien voit tout : %d", len(list))
	}

	// Accès au ticket d'autrui : introuvable (pas « interdit »), y compris en commentaire.
	if _, err := e.svc.GetTicket(ctx, e.emp2, t1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetTicket d'autrui : %v", err)
	}
	if _, err := e.svc.AddComment(ctx, e.emp2, t1.ID, "coucou"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AddComment d'autrui : %v", err)
	}
}

func TestCreateTicketValidationAndPriorityRules(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	var ve *domain.ValidationError

	bad := []service.TicketInput{
		{Title: "ab", Description: "description valide"},
		{Title: "Titre valide", Description: "x"},
		{Title: "Titre valide", Description: "description valide", Category: "bogus"},
		{Title: "Titre valide", Description: "description valide", Priority: "bogus"},
		{Title: strings.Repeat("é", 201), Description: "description valide"},
	}
	for i, in := range bad {
		if _, err := e.svc.CreateTicket(ctx, e.emp1, in); !errors.As(err, &ve) {
			t.Errorf("cas %d : ValidationError attendue, obtenu %v", i, err)
		}
	}
	missing := int64(999)
	if _, err := e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{Title: "Titre valide", Description: "description valide", EquipmentID: &missing}); !errors.As(err, &ve) {
		t.Errorf("équipement inconnu : %v", err)
	}

	// Un employé ne peut pas imposer la priorité ; le technicien le peut.
	in := service.TicketInput{Title: "Petite question", Description: "demande d'information sur le PC", Priority: domain.PriorityCritical}
	tk, err := e.svc.CreateTicket(ctx, e.emp1, in)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Priority == domain.PriorityCritical {
		t.Error("un employé ne doit pas pouvoir fixer la priorité « critique »")
	}
	tk, err = e.svc.CreateTicket(ctx, e.tech, in)
	if err != nil || tk.Priority != domain.PriorityCritical {
		t.Errorf("le technicien peut fixer la priorité : %+v, %v", tk, err)
	}
	// Catégorie choisie par l'employé prioritaire sur la suggestion.
	tk, _ = e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{Title: "Imprimante en panne", Description: "bourrage papier", Category: domain.CategoryOther})
	if tk.Category != domain.CategoryOther || *tk.AICategory != domain.CategoryPrinter {
		t.Errorf("catégorie %s / suggestion %v", tk.Category, tk.AICategory)
	}
}

func TestClassifierFailureDoesNotBlockCreation(t *testing.T) {
	e := newEnv(t, brokenClassifier{})
	tk, err := e.svc.CreateTicket(context.Background(), e.emp1, service.TicketInput{Title: "Panne", Description: "quelque chose ne va pas"})
	if err != nil {
		t.Fatalf("la création ne doit pas échouer : %v", err)
	}
	if tk.Category != domain.CategoryOther || tk.Priority != domain.PriorityMedium || tk.AICategory != nil {
		t.Errorf("valeurs par défaut attendues : %+v", tk)
	}
	if _, err := e.svc.Classify(context.Background(), "t", "description"); err == nil {
		t.Error("Classify doit propager l'erreur du classifieur")
	}
}

func TestUpdateTicket(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	tk, _ := e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{Title: "Titre valide", Description: "description valide"})
	eq, _ := e.svc.CreateEquipment(ctx, e.tech, service.EquipmentInput{AssetTag: "PC-1", Name: "PC compta", Type: domain.EquipComputer})

	prio, cat := domain.PriorityHigh, domain.CategoryComputer
	updated, err := e.svc.UpdateTicket(ctx, e.tech, tk.ID, service.TicketPatch{Priority: &prio, Category: &cat, EquipmentID: &eq.ID, AssigneeID: &e.tech.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Priority != prio || updated.Category != cat || updated.EquipmentTag != "PC-1" || *updated.AssigneeID != e.tech.ID {
		t.Errorf("mise à jour : %+v", updated)
	}
	zero := int64(0)
	updated, err = e.svc.UpdateTicket(ctx, e.tech, tk.ID, service.TicketPatch{EquipmentID: &zero, AssigneeID: &zero})
	if err != nil || updated.EquipmentID != nil || updated.AssigneeID != nil {
		t.Errorf("effacement : %+v, %v", updated, err)
	}

	var ve *domain.ValidationError
	// Un employé ne peut pas être assigné.
	if _, err := e.svc.UpdateTicket(ctx, e.tech, tk.ID, service.TicketPatch{AssigneeID: &e.emp2.ID}); !errors.As(err, &ve) {
		t.Errorf("assigner un employé : %v", err)
	}
	if _, err := e.svc.UpdateTicket(ctx, e.emp1, tk.ID, service.TicketPatch{Priority: &prio}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("employé : %v", err)
	}
	if _, err := e.svc.UpdateTicket(ctx, e.tech, 9999, service.TicketPatch{Priority: &prio}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ticket inconnu : %v", err)
	}
}

func TestEquipmentRules(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	in := service.EquipmentInput{AssetTag: "SW-01", Name: "Switch dépôt", Type: domain.EquipNetwork, ResponsibleID: &e.tech.ID}

	if _, err := e.svc.CreateEquipment(ctx, e.emp1, in); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("employé : %v", err)
	}
	eq, err := e.svc.CreateEquipment(ctx, e.tech, in)
	if err != nil {
		t.Fatal(err)
	}
	if eq.Status != domain.EquipActive || eq.ResponsibleName != "Technicien Un" {
		t.Errorf("valeurs par défaut/jointure : %+v", eq)
	}
	if _, err := e.svc.UpdateEquipment(ctx, e.emp1, eq.ID, in); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("employé ne doit pas modifier un équipement : %v", err)
	}
	if _, err := e.svc.CreateEquipment(ctx, e.tech, in); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("doublon : %v", err)
	}

	var ve *domain.ValidationError
	last := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	before := last.Add(-24 * time.Hour)
	bad := in
	bad.AssetTag = "SW-02"
	bad.LastMaintenanceAt, bad.NextMaintenanceAt = &last, &before
	if _, err := e.svc.CreateEquipment(ctx, e.tech, bad); !errors.As(err, &ve) {
		t.Errorf("dates incohérentes : %v", err)
	}
	ghost := int64(777)
	bad = in
	bad.AssetTag, bad.ResponsibleID = "SW-03", &ghost
	if _, err := e.svc.CreateEquipment(ctx, e.tech, bad); !errors.As(err, &ve) {
		t.Errorf("responsable inconnu : %v", err)
	}

	next := last.Add(90 * 24 * time.Hour)
	upd := in
	upd.Name, upd.Status, upd.LastMaintenanceAt, upd.NextMaintenanceAt = "Switch dépôt (renommé)", domain.EquipInRepair, &last, &next
	got, err := e.svc.UpdateEquipment(ctx, e.tech, eq.ID, upd)
	if err != nil || got.Name != "Switch dépôt (renommé)" || got.Status != domain.EquipInRepair || got.LastMaintenanceAt == nil {
		t.Errorf("mise à jour : %+v, %v", got, err)
	}
	if _, err := e.svc.UpdateEquipment(ctx, e.tech, 4242, upd); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("équipement inconnu : %v", err)
	}

	list, _ := e.svc.ListEquipment(ctx, service.EquipmentFilter{Query: "renommé"})
	if len(list) != 1 {
		t.Errorf("recherche : %d résultats", len(list))
	}
}

// marshalHidesHash renvoie true si la sérialisation JSON de l'utilisateur
// laisse fuiter le hash du mot de passe.
func marshalHidesHash(u *domain.User) (leaks bool, err error) {
	b, err := json.Marshal(u)
	if err != nil {
		return false, err
	}
	s := strings.ToLower(string(b))
	return strings.Contains(s, "pbkdf2") || strings.Contains(s, "password"), nil
}
func TestAIInsightsAccessAndCounts(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()

	_, err := e.svc.CreateTicket(ctx, e.emp1, service.TicketInput{
		Title:       "Problème réseau",
		Description: "Le Wi-Fi ne fonctionne plus",
	})
	if err != nil {
		t.Fatal(err)
	}

	insights, err := e.svc.AIInsights(ctx, e.tech)
	if err != nil {
		t.Fatalf("le technicien doit accéder aux insights : %v", err)
	}
	if insights.TicketCount != 1 {
		t.Errorf("TicketCount = %d, attendu 1", insights.TicketCount)
	}
	if insights.OpenCount != 1 {
		t.Errorf("OpenCount = %d, attendu 1", insights.OpenCount)
	}

	if _, err := e.svc.AIInsights(ctx, e.emp1); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("l'employé doit être refusé : %v", err)
	}
}
