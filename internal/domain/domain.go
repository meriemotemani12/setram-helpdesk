// Package domain contient les entités métier et les règles pures
// (transitions de statut, validations) partagées par tous les stores.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ---------- Erreurs ----------

var (
	ErrNotFound          = errors.New("ressource introuvable")
	ErrConflict          = errors.New("conflit : la ressource existe déjà")
	ErrInvalidTransition = errors.New("transition de statut invalide")
	ErrForbidden         = errors.New("action non autorisée")
	ErrUnauthorized      = errors.New("authentification requise")
)

// ValidationError décrit une erreur de saisie exploitable côté client.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ---------- Rôles ----------

type Role string

const (
	RoleEmployee   Role = "employee"
	RoleTechnician Role = "technician"
	RoleAdmin      Role = "admin"
)

func (r Role) Valid() bool {
	return r == RoleEmployee || r == RoleTechnician || r == RoleAdmin
}

// IsStaff vaut vrai pour les rôles qui traitent les demandes (technicien, admin).
func (r Role) IsStaff() bool { return r == RoleTechnician || r == RoleAdmin }

// ---------- Utilisateurs ----------

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"full_name"`
	Role         Role      `json:"role"`
	PasswordHash string    `json:"-"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
}

// ---------- Tickets ----------

type Status string

const (
	StatusNew        Status = "new"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

func (s Status) Valid() bool {
	return s == StatusNew || s == StatusInProgress || s == StatusDone
}

type Category string

const (
	CategoryComputer  Category = "computer"
	CategoryNetwork   Category = "network"
	CategoryPrinter   Category = "printer"
	CategoryTicketing Category = "ticketing_counter"
	CategoryOther     Category = "other"
)

var Categories = []Category{CategoryComputer, CategoryNetwork, CategoryPrinter, CategoryTicketing, CategoryOther}

func (c Category) Valid() bool {
	for _, v := range Categories {
		if v == c {
			return true
		}
	}
	return false
}

type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

var Priorities = []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical}

func (p Priority) Valid() bool {
	for _, v := range Priorities {
		if v == p {
			return true
		}
	}
	return false
}

type Ticket struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Category    Category   `json:"category"`
	Priority    Priority   `json:"priority"`
	Status      Status     `json:"status"`
	RequesterID int64      `json:"requester_id"`
	AssigneeID  *int64     `json:"assignee_id"`
	EquipmentID *int64     `json:"equipment_id"`
	AICategory  *Category  `json:"ai_category"`
	AIPriority  *Priority  `json:"ai_priority"`
	AISummary   *string    `json:"ai_summary"`
	AISource    *string    `json:"ai_source"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`

	// Champs de lecture (jointures), non persistés tels quels.
	RequesterName string `json:"requester_name,omitempty"`
	AssigneeName  string `json:"assignee_name,omitempty"`
	EquipmentTag  string `json:"equipment_tag,omitempty"`
}

// allowedTransitions décrit le workflow : nouveau → en cours → terminé,
// avec retour arrière autorisé (remise en file, réouverture).
var allowedTransitions = map[Status][]Status{
	StatusNew:        {StatusInProgress},
	StatusInProgress: {StatusDone, StatusNew},
	StatusDone:       {StatusInProgress},
}

// CanTransition indique si le passage from → to est autorisé.
func CanTransition(from, to Status) bool {
	for _, s := range allowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// ApplyTransition applique un changement de statut sur t (fonction pure,
// partagée par tous les stores) et met à jour les horodatages et l'assigné.
//
//   - new → in_progress : assigne le technicien si le ticket n'a pas d'assigné,
//     renseigne started_at à la première prise en charge.
//   - in_progress → done : renseigne resolved_at.
//   - done → in_progress : réouverture, resolved_at est effacé.
//   - in_progress → new : remise en file, l'assigné est retiré.
func ApplyTransition(t *Ticket, to Status, actorID int64, now time.Time) error {
	if !to.Valid() {
		return &ValidationError{Field: "status", Message: "statut inconnu"}
	}
	if !CanTransition(t.Status, to) {
		return fmt.Errorf("%w : %s → %s", ErrInvalidTransition, t.Status, to)
	}
	switch to {
	case StatusInProgress:
		if t.AssigneeID == nil {
			id := actorID
			t.AssigneeID = &id
		}
		if t.StartedAt == nil {
			n := now
			t.StartedAt = &n
		}
		t.ResolvedAt = nil
	case StatusDone:
		n := now
		t.ResolvedAt = &n
	case StatusNew:
		t.AssigneeID = nil
	}
	t.Status = to
	t.UpdatedAt = now
	return nil
}

// ResolutionDuration renvoie la durée création → résolution, si le ticket est résolu.
func (t *Ticket) ResolutionDuration() (time.Duration, bool) {
	if t.Status != StatusDone || t.ResolvedAt == nil {
		return 0, false
	}
	return t.ResolvedAt.Sub(t.CreatedAt), true
}

type StatusChange struct {
	ID          int64     `json:"id"`
	TicketID    int64     `json:"ticket_id"`
	FromStatus  *Status   `json:"from_status"`
	ToStatus    Status    `json:"to_status"`
	ChangedBy   int64     `json:"changed_by"`
	ChangedAt   time.Time `json:"changed_at"`
	ChangerName string    `json:"changer_name,omitempty"`
}

type Comment struct {
	ID         int64     `json:"id"`
	TicketID   int64     `json:"ticket_id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name,omitempty"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// ---------- Équipements ----------

type EquipmentType string

const (
	EquipComputer      EquipmentType = "computer"
	EquipNetwork       EquipmentType = "network"
	EquipPrinter       EquipmentType = "printer"
	EquipTicketMachine EquipmentType = "ticket_machine"
	EquipOther         EquipmentType = "other"
)

func (t EquipmentType) Valid() bool {
	switch t {
	case EquipComputer, EquipNetwork, EquipPrinter, EquipTicketMachine, EquipOther:
		return true
	}
	return false
}

type EquipmentStatus string

const (
	EquipActive   EquipmentStatus = "active"
	EquipInRepair EquipmentStatus = "in_repair"
	EquipRetired  EquipmentStatus = "retired"
)

func (s EquipmentStatus) Valid() bool {
	return s == EquipActive || s == EquipInRepair || s == EquipRetired
}

type Equipment struct {
	ID                int64           `json:"id"`
	AssetTag          string          `json:"asset_tag"`
	Name              string          `json:"name"`
	Type              EquipmentType   `json:"type"`
	Location          string          `json:"location"`
	ResponsibleID     *int64          `json:"responsible_id"`
	ResponsibleName   string          `json:"responsible_name,omitempty"`
	Status            EquipmentStatus `json:"status"`
	LastMaintenanceAt *time.Time      `json:"last_maintenance_at"`
	NextMaintenanceAt *time.Time      `json:"next_maintenance_at"`
	Notes             string          `json:"notes"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// MaintenanceOverdue vaut vrai si la prochaine maintenance est dépassée.
func (e *Equipment) MaintenanceOverdue(now time.Time) bool {
	return e.Status == EquipActive && e.NextMaintenanceAt != nil && e.NextMaintenanceAt.Before(now)
}

// ---------- Statistiques ----------

type CategoryCount struct {
	Category Category `json:"category"`
	Count    int      `json:"count"`
}

type StatusCount struct {
	Status Status `json:"status"`
	Count  int    `json:"count"`
}

type EquipmentFailures struct {
	EquipmentID int64  `json:"equipment_id"`
	AssetTag    string `json:"asset_tag"`
	Name        string `json:"name"`
	Count       int    `json:"count"`
}

type Stats struct {
	TotalTickets          int                 `json:"total_tickets"`
	ByStatus              []StatusCount       `json:"by_status"`
	ByCategory            []CategoryCount     `json:"by_category"`
	AvgResolutionHours    float64             `json:"avg_resolution_hours"`
	AvgFirstResponseHours float64             `json:"avg_first_response_hours"`
	ResolvedCount         int                 `json:"resolved_count"`
	TopEquipment          []EquipmentFailures `json:"top_equipment"`
	OverdueMaintenance    int                 `json:"overdue_maintenance"`
}

// ---------- Validation ----------

// Clean nettoie une chaîne saisie (espaces superflus).
func Clean(s string) string { return strings.TrimSpace(s) }
