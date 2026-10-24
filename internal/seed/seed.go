// Package seed insère des données de démonstration (développement uniquement).
package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/service"
)

// Demo crée des comptes, équipements et tickets de démonstration en
// rejouant le temps (service.Now est temporairement reculé) afin que le
// tableau de bord affiche des durées réalistes. Sans effet si des
// utilisateurs existent déjà. L'administrateur doit déjà exister.
func Demo(ctx context.Context, svc *service.Service, admin service.Actor, password string) error {
	users, err := svc.ListUsers(ctx, admin)
	if err != nil {
		return err
	}
	if len(users) > 1 {
		return nil // déjà peuplé
	}

	realNow := svc.Now
	defer func() { svc.Now = realNow }()
	base := realNow().Add(-14 * 24 * time.Hour)
	clock := base
	svc.Now = func() time.Time { return clock }
	at := func(day int, hour, min int) {
		clock = time.Date(base.Year(), base.Month(), base.Day(), hour, min, 0, 0, base.Location()).AddDate(0, 0, day)
	}

	mkUser := func(email, name string, role domain.Role) (service.Actor, error) {
		u, err := svc.CreateUser(ctx, admin, service.UserInput{Email: email, FullName: name, Role: role, Password: password})
		if err != nil {
			return service.Actor{}, err
		}
		return service.Actor{ID: u.ID, Role: u.Role}, nil
	}
	tech1, err := mkUser("technicien1@setram.test", "Karim Technicien", domain.RoleTechnician)
	if err != nil {
		return err
	}
	tech2, err := mkUser("technicien2@setram.test", "Sara Technicienne", domain.RoleTechnician)
	if err != nil {
		return err
	}
	emp1, err := mkUser("employe1@setram.test", "Yacine Employé", domain.RoleEmployee)
	if err != nil {
		return err
	}
	emp2, err := mkUser("employe2@setram.test", "Amel Employée", domain.RoleEmployee)
	if err != nil {
		return err
	}

	type eqSpec struct {
		tag, name, loc string
		typ            domain.EquipmentType
		resp           *service.Actor
		lastDays       int // jours écoulés depuis la dernière maintenance
		nextDays       int // négatif : jours restants avant l'échéance ; positif : jours de retard
	}
	specs := []eqSpec{
		{"GUI-001", "Distributeur de billets — Gare du Sud", "Gare du Sud", domain.EquipTicketMachine, &tech1, 100, -10},
		{"GUI-002", "Guichet vente — Gare ferroviaire", "Gare ferroviaire", domain.EquipTicketMachine, &tech2, 40, -50},
		{"PC-001", "Poste comptabilité", "Siège administratif", domain.EquipComputer, &emp1, 30, -60},
		{"PC-002", "Poste régulation (PCC)", "Poste de commande centralisé", domain.EquipComputer, &tech1, 200, 5},
		{"IMP-001", "Imprimante réseau — Dépôt", "Dépôt", domain.EquipPrinter, &tech2, 90, -20},
		{"SW-001", "Switch cœur de réseau", "Local technique", domain.EquipNetwork, &tech1, 60, -30},
	}
	eqIDs := map[string]int64{}
	for _, sp := range specs {
		last := realNow().AddDate(0, 0, -sp.lastDays)
		next := realNow().AddDate(0, 0, -sp.nextDays) // nextDays > 0 : échéance dépassée
		in := service.EquipmentInput{
			AssetTag: sp.tag, Name: sp.name, Type: sp.typ, Location: sp.loc,
			LastMaintenanceAt: &last, NextMaintenanceAt: &next,
		}
		if sp.resp != nil {
			id := sp.resp.ID
			in.ResponsibleID = &id
		}
		e, err := svc.CreateEquipment(ctx, tech1, in)
		if err != nil {
			return fmt.Errorf("équipement %s : %w", sp.tag, err)
		}
		eqIDs[sp.tag] = e.ID
	}
	ref := func(tag string) *int64 { id := eqIDs[tag]; return &id }

	type tkSpec struct {
		day, hour  int
		by         service.Actor
		title      string
		desc       string
		equip      string
		takeAfter  time.Duration // 0 = reste « nouveau »
		solveAfter time.Duration // 0 = reste « en cours » (si takeAfter > 0)
		tech       service.Actor
	}
	m := time.Minute
	h := time.Hour
	tickets := []tkSpec{
		{1, 8, emp1, "Distributeur bloqué", "Le distributeur de billets ne fonctionne pas, l'écran reste figé", "GUI-001", 20 * m, 3 * h, tech1},
		{2, 9, emp2, "Imprimante : bourrage papier", "Bourrage papier récurrent sur l'imprimante du dépôt", "IMP-001", 2 * h, 5 * h, tech2},
		{3, 10, emp1, "Perte de connexion réseau", "Plus de connexion internet dans le bureau de la comptabilité", "SW-001", 15 * m, 4 * h, tech1},
		{4, 8, emp2, "Guichet en panne", "Le guichet de la gare ferroviaire est hors service, urgent", "GUI-002", 10 * m, 90 * m, tech2},
		{5, 14, emp1, "PC très lent", "Mon ordinateur démarre très lentement depuis la mise à jour", "PC-001", 3 * h, 26 * h, tech1},
		{6, 9, emp2, "Imprimante sans toner", "Plus de toner dans l'imprimante réseau", "IMP-001", 40 * m, 2 * h, tech2},
		{8, 11, emp1, "Distributeur : lecteur de cartes", "Le distributeur de billets refuse les cartes, panne du lecteur", "GUI-001", 30 * m, 6 * h, tech1},
		{9, 8, emp2, "الشبكة بطيئة", "الشبكة بطيئة جدا في مكتب الاستقبال", "SW-001", 50 * m, 0, tech2},
		{11, 15, emp1, "Écran du poste de régulation", "L'écran du poste de régulation clignote par intermittence", "PC-002", 25 * m, 0, tech1},
		{13, 9, emp2, "Imprimante hors service", "L'imprimante du dépôt ne fonctionne plus du tout", "IMP-001", 0, 0, tech2},
		{13, 10, emp1, "Question logiciel", "Question : comment installer le logiciel de bureautique sur un nouveau poste ?", "", 0, 0, tech1},
	}
	for _, sp := range tickets {
		at(sp.day, sp.hour, 0)
		in := service.TicketInput{Title: sp.title, Description: sp.desc}
		if sp.equip != "" {
			in.EquipmentID = ref(sp.equip)
		}
		t, err := svc.CreateTicket(ctx, sp.by, in)
		if err != nil {
			return fmt.Errorf("ticket %q : %w", sp.title, err)
		}
		if sp.takeAfter == 0 {
			continue
		}
		clock = clock.Add(sp.takeAfter)
		if _, err := svc.ChangeStatus(ctx, sp.tech, t.ID, domain.StatusInProgress); err != nil {
			return err
		}
		if _, err := svc.AddComment(ctx, sp.tech, t.ID, "Pris en charge, diagnostic en cours."); err != nil {
			return err
		}
		if sp.solveAfter == 0 {
			continue
		}
		clock = clock.Add(sp.solveAfter - sp.takeAfter)
		if _, err := svc.ChangeStatus(ctx, sp.tech, t.ID, domain.StatusDone); err != nil {
			return err
		}
	}
	return nil
}
