// Package service contient la logique métier : validation, autorisations,
// workflow des tickets. Il ne dépend d'aucune base concrète (interface Store).
package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/meriem/setram-helpdesk/internal/ai"
	"github.com/meriem/setram-helpdesk/internal/auth"
	"github.com/meriem/setram-helpdesk/internal/domain"
)

// Actor est l'utilisateur authentifié qui effectue l'action.
type Actor struct {
	ID   int64
	Role domain.Role
}

type Service struct {
	store      Store
	tokens     *auth.TokenManager
	classifier ai.Classifier
	Now        func() time.Time
	dummyHash  string // comparé quand l'e-mail est inconnu, pour égaliser les temps de réponse
}

func New(store Store, tokens *auth.TokenManager, classifier ai.Classifier) (*Service, error) {
	if classifier == nil {
		classifier = ai.KeywordClassifier{}
	}
	dummy, err := auth.HashPassword("dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	return &Service{store: store, tokens: tokens, classifier: classifier, Now: time.Now, dummyHash: dummy}, nil
}

// ---------- Validation ----------

func verr(field, msg string) error { return &domain.ValidationError{Field: field, Message: msg} }

func checkLen(field, v string, min, max int) error {
	n := utf8.RuneCountInString(v)
	if n < min {
		return verr(field, "trop court")
	}
	if n > max {
		return verr(field, "trop long")
	}
	return nil
}

func normEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func requireStaff(a Actor) error {
	if !a.Role.IsStaff() {
		return domain.ErrForbidden
	}
	return nil
}

func requireAdmin(a Actor) error {
	if a.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	return nil
}

// ---------- Authentification et utilisateurs ----------

type LoginResult struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      *domain.User `json:"user"`
}

// Login vérifie les identifiants. Toute erreur renvoie ErrUnauthorized, sans
// distinguer e-mail inconnu, compte désactivé et mauvais mot de passe.
func (s *Service) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	u, err := s.store.GetUserByEmail(ctx, normEmail(email))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			auth.CheckPassword(password, s.dummyHash)
			return nil, domain.ErrUnauthorized
		}
		return nil, err
	}
	if !auth.CheckPassword(password, u.PasswordHash) || !u.Active {
		return nil, domain.ErrUnauthorized
	}
	tok, exp, err := s.tokens.Issue(u.ID, string(u.Role))
	if err != nil {
		return nil, err
	}
	return &LoginResult{Token: tok, ExpiresAt: exp, User: u}, nil
}

// Authenticate valide un jeton et recharge l'utilisateur (rôle et statut à jour).
func (s *Service) Authenticate(ctx context.Context, token string) (Actor, error) {
	c, err := s.tokens.Parse(token)
	if err != nil {
		return Actor{}, domain.ErrUnauthorized
	}
	u, err := s.store.GetUserByID(ctx, c.Subject)
	if err != nil || !u.Active {
		return Actor{}, domain.ErrUnauthorized
	}
	return Actor{ID: u.ID, Role: u.Role}, nil
}

func (s *Service) Me(ctx context.Context, a Actor) (*domain.User, error) {
	return s.store.GetUserByID(ctx, a.ID)
}

type UserInput struct {
	Email    string      `json:"email"`
	FullName string      `json:"full_name"`
	Role     domain.Role `json:"role"`
	Password string      `json:"password"`
}

func (s *Service) createUser(ctx context.Context, in UserInput) (*domain.User, error) {
	email := normEmail(in.Email)
	if !validEmail(email) {
		return nil, verr("email", "adresse e-mail invalide")
	}
	name := domain.Clean(in.FullName)
	if err := checkLen("full_name", name, 2, 100); err != nil {
		return nil, err
	}
	if !in.Role.Valid() {
		return nil, verr("role", "rôle inconnu")
	}
	if n := utf8.RuneCountInString(in.Password); n < auth.MinPasswordLength || n > 128 {
		return nil, verr("password", "entre 8 et 128 caractères")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	u := &domain.User{Email: email, FullName: name, Role: in.Role, PasswordHash: hash, Active: true, CreatedAt: s.Now()}
	if err := s.store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) CreateUser(ctx context.Context, a Actor, in UserInput) (*domain.User, error) {
	if err := requireAdmin(a); err != nil {
		return nil, err
	}
	return s.createUser(ctx, in)
}

// EnsureAdmin crée le premier administrateur si la base ne contient aucun utilisateur.
// Renvoie true si le compte a été créé.
func (s *Service) EnsureAdmin(ctx context.Context, email, password, fullName string) (bool, error) {
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := s.createUser(ctx, UserInput{Email: email, FullName: fullName, Role: domain.RoleAdmin, Password: password}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) ListUsers(ctx context.Context, a Actor) ([]*domain.User, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	return s.store.ListUsers(ctx)
}

// ---------- Tickets ----------

type TicketInput struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    domain.Category `json:"category"`
	Priority    domain.Priority `json:"priority"`
	EquipmentID *int64          `json:"equipment_id"`
}

// Classify renvoie une suggestion de catégorie et de priorité (aperçu dans le formulaire).
func (s *Service) Classify(ctx context.Context, title, description string) (*ai.Suggestion, error) {
	title, description = domain.Clean(title), domain.Clean(description)
	if err := checkLen("description", description, 3, 5000); err != nil {
		return nil, err
	}
	return s.classifier.Classify(ctx, title, description)
}

func (s *Service) CreateTicket(ctx context.Context, a Actor, in TicketInput) (*domain.Ticket, error) {
	title, desc := domain.Clean(in.Title), domain.Clean(in.Description)
	if err := checkLen("title", title, 3, 200); err != nil {
		return nil, err
	}
	if err := checkLen("description", desc, 5, 5000); err != nil {
		return nil, err
	}
	if in.Category != "" && !in.Category.Valid() {
		return nil, verr("category", "catégorie inconnue")
	}
	if in.Priority != "" && !in.Priority.Valid() {
		return nil, verr("priority", "priorité inconnue")
	}
	if in.EquipmentID != nil {
		if _, err := s.store.GetEquipment(ctx, *in.EquipmentID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, verr("equipment_id", "équipement introuvable")
			}
			return nil, err
		}
	}

	now := s.Now()
	t := &domain.Ticket{
		Title: title, Description: desc, Status: domain.StatusNew,
		RequesterID: a.ID, EquipmentID: in.EquipmentID, CreatedAt: now, UpdatedAt: now,
	}

	// Suggestion IA (best effort : une panne du classifieur ne bloque jamais la création).
	if sug, err := s.classifier.Classify(ctx, title, desc); err == nil && sug != nil {
		c, p, sum, src := sug.Category, sug.Priority, sug.Summary, sug.Source
		t.AICategory, t.AIPriority, t.AISummary, t.AISource = &c, &p, &sum, &src
	}

	// Catégorie : choix de l'employé, sinon suggestion, sinon « autre ».
	switch {
	case in.Category != "":
		t.Category = in.Category
	case t.AICategory != nil:
		t.Category = *t.AICategory
	default:
		t.Category = domain.CategoryOther
	}
	// Priorité : seul le personnel technique la fixe ; sinon suggestion, sinon « moyenne ».
	switch {
	case in.Priority != "" && a.Role.IsStaff():
		t.Priority = in.Priority
	case t.AIPriority != nil:
		t.Priority = *t.AIPriority
	default:
		t.Priority = domain.PriorityMedium
	}

	if err := s.store.CreateTicket(ctx, t); err != nil {
		return nil, err
	}
	return s.store.GetTicket(ctx, t.ID)
}

func canView(a Actor, t *domain.Ticket) bool {
	return a.Role.IsStaff() || t.RequesterID == a.ID
}

type TicketDetail struct {
	Ticket   *domain.Ticket         `json:"ticket"`
	Comments []*domain.Comment      `json:"comments"`
	History  []*domain.StatusChange `json:"history"`
}

// loadVisible charge un ticket. Un employé qui demande le ticket d'un autre
// reçoit ErrNotFound (et non ErrForbidden) pour ne pas révéler son existence.
func (s *Service) loadVisible(ctx context.Context, a Actor, id int64) (*domain.Ticket, error) {
	t, err := s.store.GetTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	if !canView(a, t) {
		return nil, domain.ErrNotFound
	}
	return t, nil
}

func (s *Service) GetTicket(ctx context.Context, a Actor, id int64) (*TicketDetail, error) {
	t, err := s.loadVisible(ctx, a, id)
	if err != nil {
		return nil, err
	}
	comments, err := s.store.ListComments(ctx, id)
	if err != nil {
		return nil, err
	}
	history, err := s.store.ListHistory(ctx, id)
	if err != nil {
		return nil, err
	}
	return &TicketDetail{Ticket: t, Comments: comments, History: history}, nil
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

func (s *Service) ListTickets(ctx context.Context, a Actor, f TicketFilter) ([]*domain.Ticket, error) {
	if !a.Role.IsStaff() {
		id := a.ID
		f.RequesterID = &id // un employé ne voit que ses propres demandes
		f.AssigneeID = nil
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	f.Query = domain.Clean(f.Query)
	return s.store.ListTickets(ctx, f)
}

// ChangeStatus fait avancer le workflow (nouveau → en cours → terminé).
func (s *Service) ChangeStatus(ctx context.Context, a Actor, id int64, to domain.Status) (*domain.Ticket, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	if !to.Valid() {
		return nil, verr("status", "statut inconnu")
	}
	return s.store.TransitionTicket(ctx, id, to, a.ID, s.Now())
}

// TicketPatch : les champs absents (nil) sont inchangés. Pour equipment_id et
// assignee_id, la valeur 0 efface l'association.
type TicketPatch struct {
	Priority    *domain.Priority `json:"priority"`
	Category    *domain.Category `json:"category"`
	EquipmentID *int64           `json:"equipment_id"`
	AssigneeID  *int64           `json:"assignee_id"`
}

func (s *Service) UpdateTicket(ctx context.Context, a Actor, id int64, p TicketPatch) (*domain.Ticket, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	t, err := s.store.GetTicket(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Priority != nil {
		if !p.Priority.Valid() {
			return nil, verr("priority", "priorité inconnue")
		}
		t.Priority = *p.Priority
	}
	if p.Category != nil {
		if !p.Category.Valid() {
			return nil, verr("category", "catégorie inconnue")
		}
		t.Category = *p.Category
	}
	if p.EquipmentID != nil {
		if *p.EquipmentID == 0 {
			t.EquipmentID = nil
		} else {
			if _, err := s.store.GetEquipment(ctx, *p.EquipmentID); err != nil {
				if errors.Is(err, domain.ErrNotFound) {
					return nil, verr("equipment_id", "équipement introuvable")
				}
				return nil, err
			}
			v := *p.EquipmentID
			t.EquipmentID = &v
		}
	}
	if p.AssigneeID != nil {
		if *p.AssigneeID == 0 {
			t.AssigneeID = nil
		} else {
			u, err := s.store.GetUserByID(ctx, *p.AssigneeID)
			if err != nil || !u.Active || !u.Role.IsStaff() {
				return nil, verr("assignee_id", "doit désigner un technicien actif")
			}
			v := *p.AssigneeID
			t.AssigneeID = &v
		}
	}
	t.UpdatedAt = s.Now()
	if err := s.store.SaveTicketFields(ctx, t); err != nil {
		return nil, err
	}
	return s.store.GetTicket(ctx, id)
}

func (s *Service) AddComment(ctx context.Context, a Actor, ticketID int64, body string) (*domain.Comment, error) {
	body = domain.Clean(body)
	if err := checkLen("body", body, 1, 2000); err != nil {
		return nil, err
	}
	if _, err := s.loadVisible(ctx, a, ticketID); err != nil {
		return nil, err
	}
	c := &domain.Comment{TicketID: ticketID, AuthorID: a.ID, Body: body, CreatedAt: s.Now()}
	if err := s.store.AddComment(ctx, c); err != nil {
		return nil, err
	}
	if u, err := s.store.GetUserByID(ctx, a.ID); err == nil {
		c.AuthorName = u.FullName
	}
	return c, nil
}

// ---------- Équipements ----------

type EquipmentInput struct {
	AssetTag          string                 `json:"asset_tag"`
	Name              string                 `json:"name"`
	Type              domain.EquipmentType   `json:"type"`
	Location          string                 `json:"location"`
	ResponsibleID     *int64                 `json:"responsible_id"`
	Status            domain.EquipmentStatus `json:"status"`
	LastMaintenanceAt *time.Time             `json:"last_maintenance_at"`
	NextMaintenanceAt *time.Time             `json:"next_maintenance_at"`
	Notes             string                 `json:"notes"`
}

func (s *Service) buildEquipment(ctx context.Context, in EquipmentInput) (*domain.Equipment, error) {
	tag := domain.Clean(in.AssetTag)
	if err := checkLen("asset_tag", tag, 2, 50); err != nil {
		return nil, err
	}
	name := domain.Clean(in.Name)
	if err := checkLen("name", name, 2, 150); err != nil {
		return nil, err
	}
	if !in.Type.Valid() {
		return nil, verr("type", "type inconnu")
	}
	status := in.Status
	if status == "" {
		status = domain.EquipActive
	}
	if !status.Valid() {
		return nil, verr("status", "statut inconnu")
	}
	loc, notes := domain.Clean(in.Location), domain.Clean(in.Notes)
	if err := checkLen("location", loc, 0, 150); err != nil {
		return nil, err
	}
	if err := checkLen("notes", notes, 0, 2000); err != nil {
		return nil, err
	}
	if in.LastMaintenanceAt != nil && in.NextMaintenanceAt != nil && in.NextMaintenanceAt.Before(*in.LastMaintenanceAt) {
		return nil, verr("next_maintenance_at", "doit être postérieure à la dernière maintenance")
	}
	if in.ResponsibleID != nil {
		u, err := s.store.GetUserByID(ctx, *in.ResponsibleID)
		if err != nil || !u.Active {
			return nil, verr("responsible_id", "utilisateur introuvable")
		}
	}
	return &domain.Equipment{
		AssetTag: tag, Name: name, Type: in.Type, Location: loc, ResponsibleID: in.ResponsibleID,
		Status: status, LastMaintenanceAt: in.LastMaintenanceAt, NextMaintenanceAt: in.NextMaintenanceAt, Notes: notes,
	}, nil
}

func (s *Service) CreateEquipment(ctx context.Context, a Actor, in EquipmentInput) (*domain.Equipment, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	e, err := s.buildEquipment(ctx, in)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	e.CreatedAt, e.UpdatedAt = now, now
	if err := s.store.CreateEquipment(ctx, e); err != nil {
		return nil, err
	}
	return s.store.GetEquipment(ctx, e.ID)
}

func (s *Service) UpdateEquipment(ctx context.Context, a Actor, id int64, in EquipmentInput) (*domain.Equipment, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	cur, err := s.store.GetEquipment(ctx, id)
	if err != nil {
		return nil, err
	}
	e, err := s.buildEquipment(ctx, in)
	if err != nil {
		return nil, err
	}
	e.ID, e.CreatedAt, e.UpdatedAt = id, cur.CreatedAt, s.Now()
	if err := s.store.UpdateEquipment(ctx, e); err != nil {
		return nil, err
	}
	return s.store.GetEquipment(ctx, id)
}

func (s *Service) GetEquipment(ctx context.Context, id int64) (*domain.Equipment, error) {
	return s.store.GetEquipment(ctx, id)
}

func (s *Service) ListEquipment(ctx context.Context, f EquipmentFilter) ([]*domain.Equipment, error) {
	f.Query = domain.Clean(f.Query)
	return s.store.ListEquipment(ctx, f)
}

// ---------- Statistiques ----------

func (s *Service) Stats(ctx context.Context, a Actor) (*domain.Stats, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}
	return s.store.Stats(ctx, s.Now())
}
