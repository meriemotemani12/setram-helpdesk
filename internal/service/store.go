package service

import (
	"context"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
)

// TicketFilter regroupe les critères de recherche de tickets.
type TicketFilter struct {
	Status      *domain.Status
	Category    *domain.Category
	RequesterID *int64
	AssigneeID  *int64
	EquipmentID *int64
	Query       string // recherche dans le titre et la description
	Limit       int
	Offset      int
}

// EquipmentFilter regroupe les critères de recherche d'équipements.
type EquipmentFilter struct {
	Type   *domain.EquipmentType
	Status *domain.EquipmentStatus
	Query  string // recherche dans le nom, le numéro d'inventaire et l'emplacement
}

// Store est la couche de persistance. Implémentations : postgres (production)
// et memory (tests). Les erreurs métier doivent être domain.ErrNotFound,
// domain.ErrConflict ou domain.ErrInvalidTransition.
type Store interface {
	// Utilisateurs
	CreateUser(ctx context.Context, u *domain.User) error
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByID(ctx context.Context, id int64) (*domain.User, error)
	ListUsers(ctx context.Context) ([]*domain.User, error)
	CountUsers(ctx context.Context) (int, error)

	// Équipements
	CreateEquipment(ctx context.Context, e *domain.Equipment) error
	UpdateEquipment(ctx context.Context, e *domain.Equipment) error
	GetEquipment(ctx context.Context, id int64) (*domain.Equipment, error)
	ListEquipment(ctx context.Context, f EquipmentFilter) ([]*domain.Equipment, error)

	// Tickets
	// CreateTicket insère le ticket et la première ligne d'historique (nil → new).
	CreateTicket(ctx context.Context, t *domain.Ticket) error
	GetTicket(ctx context.Context, id int64) (*domain.Ticket, error)
	ListTickets(ctx context.Context, f TicketFilter) ([]*domain.Ticket, error)
	// SaveTicketFields met à jour priorité, catégorie, assigné et équipement (pas le statut).
	SaveTicketFields(ctx context.Context, t *domain.Ticket) error
	// TransitionTicket applique atomiquement un changement de statut
	// (verrou de ligne, domain.ApplyTransition, ligne d'historique).
	TransitionTicket(ctx context.Context, id int64, to domain.Status, actorID int64, now time.Time) (*domain.Ticket, error)

	AddComment(ctx context.Context, c *domain.Comment) error
	ListComments(ctx context.Context, ticketID int64) ([]*domain.Comment, error)
	ListHistory(ctx context.Context, ticketID int64) ([]*domain.StatusChange, error)

	Stats(ctx context.Context, now time.Time) (*domain.Stats, error)
}
