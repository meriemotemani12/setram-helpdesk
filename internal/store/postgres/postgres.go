// Package postgres implémente service.Store sur PostgreSQL (pgx v5).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/service"
)

type Store struct {
	pool *pgxpool.Pool
}

var _ service.Store = (*Store)(nil)

// New ouvre le pool de connexions et vérifie qu'il répond.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pool postgres : %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres : %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Pool expose le pool (utilisé par le migrateur).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// mapErr traduit les erreurs PostgreSQL en erreurs métier.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505": // unique_violation
			return domain.ErrConflict
		case "23503": // foreign_key_violation : l'entité référencée n'existe pas
			return domain.ErrNotFound
		}
	}
	return err
}

// escapeLike protège %, _ et \ dans un motif ILIKE.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// where construit une clause WHERE avec des paramètres numérotés.
type where struct {
	conds []string
	args  []any
}

func (w *where) add(cond string, arg any) {
	w.args = append(w.args, arg)
	w.conds = append(w.conds, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(w.args))))
}

func (w *where) sql() string {
	if len(w.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(w.conds, " AND ")
}

func optStr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

// ---------- Utilisateurs ----------

const userCols = `id, email, full_name, role, password_hash, active, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(r scanner) (*domain.User, error) {
	var u domain.User
	var role string
	if err := r.Scan(&u.ID, &u.Email, &u.FullName, &role, &u.PasswordHash, &u.Active, &u.CreatedAt); err != nil {
		return nil, mapErr(err)
	}
	u.Role = domain.Role(role)
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, u *domain.User) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (email, full_name, role, password_hash, active, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		u.Email, u.FullName, string(u.Role), u.PasswordHash, u.Active, u.CreatedAt).Scan(&u.ID)
	return mapErr(err)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*domain.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

func (s *Store) ListUsers(ctx context.Context) ([]*domain.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []*domain.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, mapErr(rows.Err())
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, mapErr(err)
}

// ---------- Équipements ----------

const equipmentSelect = `
SELECT e.id, e.asset_tag, e.name, e.type, e.location, e.responsible_id, COALESCE(u.full_name, ''),
       e.status, e.last_maintenance_at, e.next_maintenance_at, e.notes, e.created_at, e.updated_at
FROM equipment e
LEFT JOIN users u ON u.id = e.responsible_id`

func scanEquipment(r scanner) (*domain.Equipment, error) {
	var e domain.Equipment
	var typ, status string
	if err := r.Scan(&e.ID, &e.AssetTag, &e.Name, &typ, &e.Location, &e.ResponsibleID, &e.ResponsibleName,
		&status, &e.LastMaintenanceAt, &e.NextMaintenanceAt, &e.Notes, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	e.Type, e.Status = domain.EquipmentType(typ), domain.EquipmentStatus(status)
	return &e, nil
}

func (s *Store) CreateEquipment(ctx context.Context, e *domain.Equipment) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO equipment (asset_tag, name, type, location, responsible_id, status,
		                        last_maintenance_at, next_maintenance_at, notes, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		e.AssetTag, e.Name, string(e.Type), e.Location, e.ResponsibleID, string(e.Status),
		e.LastMaintenanceAt, e.NextMaintenanceAt, e.Notes, e.CreatedAt, e.UpdatedAt).Scan(&e.ID)
	return mapErr(err)
}

func (s *Store) UpdateEquipment(ctx context.Context, e *domain.Equipment) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE equipment
		    SET asset_tag = $2, name = $3, type = $4, location = $5, responsible_id = $6, status = $7,
		        last_maintenance_at = $8, next_maintenance_at = $9, notes = $10, updated_at = $11
		  WHERE id = $1`,
		e.ID, e.AssetTag, e.Name, string(e.Type), e.Location, e.ResponsibleID, string(e.Status),
		e.LastMaintenanceAt, e.NextMaintenanceAt, e.Notes, e.UpdatedAt)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) GetEquipment(ctx context.Context, id int64) (*domain.Equipment, error) {
	return scanEquipment(s.pool.QueryRow(ctx, equipmentSelect+` WHERE e.id = $1`, id))
}

func (s *Store) ListEquipment(ctx context.Context, f service.EquipmentFilter) ([]*domain.Equipment, error) {
	var w where
	if f.Type != nil {
		w.add("e.type = ?", string(*f.Type))
	}
	if f.Status != nil {
		w.add("e.status = ?", string(*f.Status))
	}
	if f.Query != "" {
		w.add(`(e.name ILIKE ? OR e.asset_tag ILIKE ? OR e.location ILIKE ?)`, "%"+escapeLike(f.Query)+"%")
	}
	rows, err := s.pool.Query(ctx, equipmentSelect+w.sql()+` ORDER BY e.asset_tag`, w.args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []*domain.Equipment{}
	for rows.Next() {
		e, err := scanEquipment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, mapErr(rows.Err())
}

// ---------- Tickets ----------

const ticketBaseCols = `t.id, t.title, t.description, t.category, t.priority, t.status, t.requester_id,
       t.assignee_id, t.equipment_id, t.ai_category, t.ai_priority, t.ai_summary, t.ai_source,
       t.created_at, t.updated_at, t.started_at, t.resolved_at`

const ticketSelect = `
SELECT ` + ticketBaseCols + `,
       ru.full_name, COALESCE(au.full_name, ''), COALESCE(e.asset_tag, '')
FROM tickets t
JOIN users ru ON ru.id = t.requester_id
LEFT JOIN users au ON au.id = t.assignee_id
LEFT JOIN equipment e ON e.id = t.equipment_id`

func scanTicket(r scanner, withNames bool) (*domain.Ticket, error) {
	var t domain.Ticket
	var cat, prio, status string
	var aiCat, aiPrio *string
	dest := []any{&t.ID, &t.Title, &t.Description, &cat, &prio, &status, &t.RequesterID,
		&t.AssigneeID, &t.EquipmentID, &aiCat, &aiPrio, &t.AISummary, &t.AISource,
		&t.CreatedAt, &t.UpdatedAt, &t.StartedAt, &t.ResolvedAt}
	if withNames {
		dest = append(dest, &t.RequesterName, &t.AssigneeName, &t.EquipmentTag)
	}
	if err := r.Scan(dest...); err != nil {
		return nil, mapErr(err)
	}
	t.Category, t.Priority, t.Status = domain.Category(cat), domain.Priority(prio), domain.Status(status)
	if aiCat != nil {
		c := domain.Category(*aiCat)
		t.AICategory = &c
	}
	if aiPrio != nil {
		p := domain.Priority(*aiPrio)
		t.AIPriority = &p
	}
	return &t, nil
}

func (s *Store) CreateTicket(ctx context.Context, t *domain.Ticket) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // sans effet après Commit

	err = tx.QueryRow(ctx,
		`INSERT INTO tickets (title, description, category, priority, status, requester_id, equipment_id,
		                      ai_category, ai_priority, ai_summary, ai_source, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, 'new', $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
		t.Title, t.Description, string(t.Category), string(t.Priority), t.RequesterID, t.EquipmentID,
		optStr(t.AICategory), optStr(t.AIPriority), t.AISummary, t.AISource, t.CreatedAt, t.UpdatedAt).Scan(&t.ID)
	if err != nil {
		return mapErr(err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ticket_status_history (ticket_id, from_status, to_status, changed_by, changed_at)
		 VALUES ($1, NULL, 'new', $2, $3)`, t.ID, t.RequesterID, t.CreatedAt); err != nil {
		return mapErr(err)
	}
	t.Status = domain.StatusNew
	return mapErr(tx.Commit(ctx))
}

func (s *Store) GetTicket(ctx context.Context, id int64) (*domain.Ticket, error) {
	return scanTicket(s.pool.QueryRow(ctx, ticketSelect+` WHERE t.id = $1`, id), true)
}

func (s *Store) ListTickets(ctx context.Context, f service.TicketFilter) ([]*domain.Ticket, error) {
	var w where
	if f.Status != nil {
		w.add("t.status = ?", string(*f.Status))
	}
	if f.Category != nil {
		w.add("t.category = ?", string(*f.Category))
	}
	if f.RequesterID != nil {
		w.add("t.requester_id = ?", *f.RequesterID)
	}
	if f.AssigneeID != nil {
		w.add("t.assignee_id = ?", *f.AssigneeID)
	}
	if f.EquipmentID != nil {
		w.add("t.equipment_id = ?", *f.EquipmentID)
	}
	if f.Query != "" {
		w.add(`(t.title ILIKE ? OR t.description ILIKE ?)`, "%"+escapeLike(f.Query)+"%")
	}
	sql := ticketSelect + w.sql() + ` ORDER BY t.created_at DESC, t.id DESC`
	if f.Limit > 0 {
		w.args = append(w.args, f.Limit)
		sql += fmt.Sprintf(" LIMIT $%d", len(w.args))
	}
	if f.Offset > 0 {
		w.args = append(w.args, f.Offset)
		sql += fmt.Sprintf(" OFFSET $%d", len(w.args))
	}
	rows, err := s.pool.Query(ctx, sql, w.args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []*domain.Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, mapErr(rows.Err())
}

func (s *Store) SaveTicketFields(ctx context.Context, t *domain.Ticket) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tickets
		    SET priority = $2, category = $3, assignee_id = $4, equipment_id = $5, updated_at = $6
		  WHERE id = $1`,
		t.ID, string(t.Priority), string(t.Category), t.AssigneeID, t.EquipmentID, t.UpdatedAt)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// TransitionTicket verrouille la ligne (FOR UPDATE), applique la règle métier
// partagée domain.ApplyTransition, puis écrit le ticket et l'historique dans
// la même transaction : deux techniciens ne peuvent pas se marcher dessus.
func (s *Store) TransitionTicket(ctx context.Context, id int64, to domain.Status, actorID int64, now time.Time) (*domain.Ticket, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // sans effet après Commit

	t, err := scanTicket(tx.QueryRow(ctx, `SELECT `+ticketBaseCols+` FROM tickets t WHERE t.id = $1 FOR UPDATE`, id), false)
	if err != nil {
		return nil, err
	}
	from := t.Status
	if err := domain.ApplyTransition(t, to, actorID, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tickets
		    SET status = $2, assignee_id = $3, started_at = $4, resolved_at = $5, updated_at = $6
		  WHERE id = $1`,
		id, string(t.Status), t.AssigneeID, t.StartedAt, t.ResolvedAt, t.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ticket_status_history (ticket_id, from_status, to_status, changed_by, changed_at)
		 VALUES ($1, $2, $3, $4, $5)`, id, string(from), string(to), actorID, now); err != nil {
		return nil, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, mapErr(err)
	}
	return s.GetTicket(ctx, id)
}

// ---------- Commentaires et historique ----------

func (s *Store) AddComment(ctx context.Context, c *domain.Comment) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO comments (ticket_id, author_id, body, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		c.TicketID, c.AuthorID, c.Body, c.CreatedAt).Scan(&c.ID)
	return mapErr(err)
}

func (s *Store) ListComments(ctx context.Context, ticketID int64) ([]*domain.Comment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.ticket_id, c.author_id, u.full_name, c.body, c.created_at
		   FROM comments c JOIN users u ON u.id = c.author_id
		  WHERE c.ticket_id = $1 ORDER BY c.created_at, c.id`, ticketID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []*domain.Comment{}
	for rows.Next() {
		var c domain.Comment
		if err := rows.Scan(&c.ID, &c.TicketID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &c)
	}
	return out, mapErr(rows.Err())
}

func (s *Store) ListHistory(ctx context.Context, ticketID int64) ([]*domain.StatusChange, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT h.id, h.ticket_id, h.from_status, h.to_status, h.changed_by, u.full_name, h.changed_at
		   FROM ticket_status_history h JOIN users u ON u.id = h.changed_by
		  WHERE h.ticket_id = $1 ORDER BY h.changed_at, h.id`, ticketID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []*domain.StatusChange{}
	for rows.Next() {
		var h domain.StatusChange
		var from *string
		var to string
		if err := rows.Scan(&h.ID, &h.TicketID, &from, &to, &h.ChangedBy, &h.ChangerName, &h.ChangedAt); err != nil {
			return nil, mapErr(err)
		}
		if from != nil {
			f := domain.Status(*from)
			h.FromStatus = &f
		}
		h.ToStatus = domain.Status(to)
		out = append(out, &h)
	}
	return out, mapErr(rows.Err())
}

// ---------- Statistiques ----------

// Stats calcule en SQL les mêmes indicateurs que domain.ComputeStats.
func (s *Store) Stats(ctx context.Context, now time.Time) (*domain.Stats, error) {
	st := &domain.Stats{}

	counts := map[domain.Status]int{}
	rows, err := s.pool.Query(ctx, `SELECT status, count(*) FROM tickets GROUP BY status`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		counts[domain.Status(status)] = n
		st.TotalTickets += n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	for _, status := range []domain.Status{domain.StatusNew, domain.StatusInProgress, domain.StatusDone} {
		st.ByStatus = append(st.ByStatus, domain.StatusCount{Status: status, Count: counts[status]})
	}

	st.ByCategory = []domain.CategoryCount{}
	rows, err = s.pool.Query(ctx, `SELECT category, count(*) AS n FROM tickets GROUP BY category ORDER BY n DESC, category`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		st.ByCategory = append(st.ByCategory, domain.CategoryCount{Category: domain.Category(c), Count: n})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}

	// Durée moyenne de résolution (création → résolution) des tickets terminés.
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (resolved_at - created_at)))::float8 / 3600.0, 0)::float8, count(*)
		   FROM tickets WHERE status = 'done'`).Scan(&st.AvgResolutionHours, &st.ResolvedCount); err != nil {
		return nil, mapErr(err)
	}
	// Délai moyen de première prise en charge (création → premier passage « en cours »).
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (started_at - created_at)))::float8 / 3600.0, 0)::float8
		   FROM tickets WHERE started_at IS NOT NULL`).Scan(&st.AvgFirstResponseHours); err != nil {
		return nil, mapErr(err)
	}

	st.TopEquipment = []domain.EquipmentFailures{}
	rows, err = s.pool.Query(ctx,
		`SELECT e.id, e.asset_tag, e.name, count(*) AS n
		   FROM tickets t JOIN equipment e ON e.id = t.equipment_id
		  GROUP BY e.id, e.asset_tag, e.name
		  ORDER BY n DESC, e.id
		  LIMIT $1`, domain.TopEquipmentLimit)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var f domain.EquipmentFailures
		if err := rows.Scan(&f.EquipmentID, &f.AssetTag, &f.Name, &f.Count); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		st.TopEquipment = append(st.TopEquipment, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}

	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM equipment WHERE status = 'active' AND next_maintenance_at < $1`, now).Scan(&st.OverdueMaintenance); err != nil {
		return nil, mapErr(err)
	}
	return st, nil
}
