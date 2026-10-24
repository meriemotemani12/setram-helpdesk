// Package memory est un store en mémoire, utilisé par les tests et pour
// démarrer rapidement sans base de données (non persistant).
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/service"
)

type Store struct {
	mu        sync.Mutex
	seq       int64
	users     map[int64]*domain.User
	equipment map[int64]*domain.Equipment
	tickets   map[int64]*domain.Ticket
	comments  []*domain.Comment
	history   []*domain.StatusChange
}

var _ service.Store = (*Store)(nil)

func New() *Store {
	return &Store{
		users:     map[int64]*domain.User{},
		equipment: map[int64]*domain.Equipment{},
		tickets:   map[int64]*domain.Ticket{},
	}
}

func (s *Store) next() int64 { s.seq++; return s.seq }

func cp[T any](v *T) *T { c := *v; return &c }

// ---------- Utilisateurs ----------

func (s *Store) CreateUser(_ context.Context, u *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.users {
		if strings.EqualFold(o.Email, u.Email) {
			return domain.ErrConflict
		}
	}
	u.ID = s.next()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	s.users[u.ID] = cp(u)
	return nil
}

func (s *Store) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return cp(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *Store) GetUserByID(_ context.Context, id int64) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		return cp(u), nil
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListUsers(_ context.Context) ([]*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, cp(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) CountUsers(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users), nil
}

func (s *Store) userName(id int64) string {
	if u, ok := s.users[id]; ok {
		return u.FullName
	}
	return ""
}

// ---------- Équipements ----------

func (s *Store) fillEquipment(e *domain.Equipment) *domain.Equipment {
	c := cp(e)
	if c.ResponsibleID != nil {
		c.ResponsibleName = s.userName(*c.ResponsibleID)
	}
	return c
}

func (s *Store) CreateEquipment(_ context.Context, e *domain.Equipment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.equipment {
		if o.AssetTag == e.AssetTag {
			return domain.ErrConflict
		}
	}
	if e.ResponsibleID != nil {
		if _, ok := s.users[*e.ResponsibleID]; !ok {
			return domain.ErrNotFound
		}
	}
	e.ID = s.next()
	s.equipment[e.ID] = cp(e)
	return nil
}

func (s *Store) UpdateEquipment(_ context.Context, e *domain.Equipment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.equipment[e.ID]; !ok {
		return domain.ErrNotFound
	}
	for _, o := range s.equipment {
		if o.ID != e.ID && o.AssetTag == e.AssetTag {
			return domain.ErrConflict
		}
	}
	if e.ResponsibleID != nil {
		if _, ok := s.users[*e.ResponsibleID]; !ok {
			return domain.ErrNotFound
		}
	}
	s.equipment[e.ID] = cp(e)
	return nil
}

func (s *Store) GetEquipment(_ context.Context, id int64) (*domain.Equipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.equipment[id]; ok {
		return s.fillEquipment(e), nil
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListEquipment(_ context.Context, f service.EquipmentFilter) ([]*domain.Equipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(f.Query)
	var out []*domain.Equipment
	for _, e := range s.equipment {
		if f.Type != nil && e.Type != *f.Type {
			continue
		}
		if f.Status != nil && e.Status != *f.Status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.AssetTag+" "+e.Location), q) {
			continue
		}
		out = append(out, s.fillEquipment(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssetTag < out[j].AssetTag })
	return out, nil
}

// ---------- Tickets ----------

func (s *Store) fillTicket(t *domain.Ticket) *domain.Ticket {
	c := cp(t)
	c.RequesterName = s.userName(t.RequesterID)
	if t.AssigneeID != nil {
		c.AssigneeName = s.userName(*t.AssigneeID)
	}
	if t.EquipmentID != nil {
		if e, ok := s.equipment[*t.EquipmentID]; ok {
			c.EquipmentTag = e.AssetTag
		}
	}
	return c
}

func (s *Store) CreateTicket(_ context.Context, t *domain.Ticket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[t.RequesterID]; !ok {
		return domain.ErrNotFound
	}
	if t.EquipmentID != nil {
		if _, ok := s.equipment[*t.EquipmentID]; !ok {
			return domain.ErrNotFound
		}
	}
	t.ID = s.next()
	t.Status = domain.StatusNew
	s.tickets[t.ID] = cp(t)
	s.history = append(s.history, &domain.StatusChange{
		ID: s.next(), TicketID: t.ID, ToStatus: domain.StatusNew, ChangedBy: t.RequesterID, ChangedAt: t.CreatedAt,
	})
	return nil
}

func (s *Store) GetTicket(_ context.Context, id int64) (*domain.Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tickets[id]; ok {
		return s.fillTicket(t), nil
	}
	return nil, domain.ErrNotFound
}

func (s *Store) ListTickets(_ context.Context, f service.TicketFilter) ([]*domain.Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(f.Query)
	var out []*domain.Ticket
	for _, t := range s.tickets {
		if f.Status != nil && t.Status != *f.Status {
			continue
		}
		if f.Category != nil && t.Category != *f.Category {
			continue
		}
		if f.RequesterID != nil && t.RequesterID != *f.RequesterID {
			continue
		}
		if f.AssigneeID != nil && (t.AssigneeID == nil || *t.AssigneeID != *f.AssigneeID) {
			continue
		}
		if f.EquipmentID != nil && (t.EquipmentID == nil || *t.EquipmentID != *f.EquipmentID) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Title+" "+t.Description), q) {
			continue
		}
		out = append(out, s.fillTicket(t))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return []*domain.Ticket{}, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	if out == nil {
		out = []*domain.Ticket{}
	}
	return out, nil
}

func (s *Store) SaveTicketFields(_ context.Context, t *domain.Ticket) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.tickets[t.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if t.AssigneeID != nil {
		if _, ok := s.users[*t.AssigneeID]; !ok {
			return domain.ErrNotFound
		}
	}
	if t.EquipmentID != nil {
		if _, ok := s.equipment[*t.EquipmentID]; !ok {
			return domain.ErrNotFound
		}
	}
	cur.Priority, cur.Category = t.Priority, t.Category
	cur.AssigneeID, cur.EquipmentID = t.AssigneeID, t.EquipmentID
	cur.UpdatedAt = t.UpdatedAt
	return nil
}

func (s *Store) TransitionTicket(_ context.Context, id int64, to domain.Status, actorID int64, now time.Time) (*domain.Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.tickets[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	work := cp(cur)
	from := work.Status
	if err := domain.ApplyTransition(work, to, actorID, now); err != nil {
		return nil, err
	}
	s.tickets[id] = work
	s.history = append(s.history, &domain.StatusChange{
		ID: s.next(), TicketID: id, FromStatus: &from, ToStatus: to, ChangedBy: actorID, ChangedAt: now,
	})
	return s.fillTicket(work), nil
}

func (s *Store) AddComment(_ context.Context, c *domain.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tickets[c.TicketID]; !ok {
		return domain.ErrNotFound
	}
	c.ID = s.next()
	s.comments = append(s.comments, cp(c))
	return nil
}

func (s *Store) ListComments(_ context.Context, ticketID int64) ([]*domain.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*domain.Comment{}
	for _, c := range s.comments {
		if c.TicketID == ticketID {
			cc := cp(c)
			cc.AuthorName = s.userName(c.AuthorID)
			out = append(out, cc)
		}
	}
	return out, nil
}

func (s *Store) ListHistory(_ context.Context, ticketID int64) ([]*domain.StatusChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*domain.StatusChange{}
	for _, h := range s.history {
		if h.TicketID == ticketID {
			hh := cp(h)
			hh.ChangerName = s.userName(h.ChangedBy)
			out = append(out, hh)
		}
	}
	return out, nil
}

func (s *Store) Stats(_ context.Context, now time.Time) (*domain.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := make([]*domain.Ticket, 0, len(s.tickets))
	for _, t := range s.tickets {
		ts = append(ts, t)
	}
	es := make([]*domain.Equipment, 0, len(s.equipment))
	for _, e := range s.equipment {
		es = append(es, e)
	}
	return domain.ComputeStats(ts, es, now), nil
}
