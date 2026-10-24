// Package httpapi expose le service métier en REST/JSON et sert l'interface web.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/service"
)

const maxBodyBytes = 1 << 20 // 1 Mo

type Server struct {
	svc     *service.Service
	static  fs.FS
	log     *slog.Logger
	limiter *rateLimiter
	mux     *http.ServeMux
}

// Options configure le serveur HTTP.
type Options struct {
	Static         fs.FS         // fichiers de l'interface (peut être nil)
	Logger         *slog.Logger  // défaut : slog.Default()
	LoginRateLimit int           // tentatives de connexion par fenêtre et par IP (défaut 10)
	LoginWindow    time.Duration // fenêtre (défaut 1 min)
}

func New(svc *service.Service, opt Options) *Server {
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	if opt.LoginRateLimit <= 0 {
		opt.LoginRateLimit = 10
	}
	if opt.LoginWindow <= 0 {
		opt.LoginWindow = time.Minute
	}
	s := &Server{
		svc: svc, static: opt.Static, log: opt.Logger,
		limiter: newRateLimiter(opt.LoginRateLimit, opt.LoginWindow),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

// Handler renvoie le handler complet (middlewares inclus).
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.securityHeaders(s.logRequests(s.mux)))
}

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	m.HandleFunc("POST /api/auth/login", s.handleLogin)
	m.HandleFunc("GET /api/me", s.auth(s.handleMe))

	m.HandleFunc("GET /api/users", s.auth(s.handleListUsers))
	m.HandleFunc("POST /api/users", s.auth(s.handleCreateUser))

	m.HandleFunc("POST /api/tickets/classify", s.auth(s.handleClassify))
	m.HandleFunc("GET /api/tickets", s.auth(s.handleListTickets))
	m.HandleFunc("POST /api/tickets", s.auth(s.handleCreateTicket))
	m.HandleFunc("GET /api/tickets/{id}", s.auth(s.handleGetTicket))
	m.HandleFunc("PATCH /api/tickets/{id}", s.auth(s.handleUpdateTicket))
	m.HandleFunc("POST /api/tickets/{id}/status", s.auth(s.handleChangeStatus))
	m.HandleFunc("POST /api/tickets/{id}/comments", s.auth(s.handleAddComment))

	m.HandleFunc("GET /api/equipment", s.auth(s.handleListEquipment))
	m.HandleFunc("POST /api/equipment", s.auth(s.handleCreateEquipment))
	m.HandleFunc("GET /api/equipment/{id}", s.auth(s.handleGetEquipment))
	m.HandleFunc("PUT /api/equipment/{id}", s.auth(s.handleUpdateEquipment))

	m.HandleFunc("GET /api/stats", s.auth(s.handleStats))
	m.HandleFunc("GET /api/ai/insights", s.auth(s.handleAIInsights))

	// Toute autre route GET /api/… → 404 JSON (et non la page d'accueil).
	// (« GET /api/ » est plus spécifique que « GET / » : pas de conflit de motifs.)
	m.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errBody{Error: "route inconnue"})
	})

	if s.static != nil {
		files := http.FileServerFS(s.static)
		m.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".html") || r.URL.Path == "/" {
				w.Header().Set("Cache-Control", "no-cache")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			files.ServeHTTP(w, r)
		})
	}
}

// ---------- Middlewares ----------

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "path", r.URL.Path, "panic", rec)
				writeJSON(w, http.StatusInternalServerError, errBody{Error: "erreur interne"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) { r.status = code; r.ResponseWriter.WriteHeader(code) }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

type authedHandler func(w http.ResponseWriter, r *http.Request, a service.Actor)

// auth exige un jeton Bearer valide et injecte l'acteur.
func (s *Server) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			writeError(w, s.log, domain.ErrUnauthorized)
			return
		}
		a, err := s.svc.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, s.log, err)
			return
		}
		next(w, r, a)
	}
}

// ---------- Limitation du débit (connexion) ----------

type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*bucket
	now    func() time.Time
}

type bucket struct {
	count int
	reset time.Time
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: map[string]*bucket{}, now: time.Now}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.hits) > 10_000 { // évite une croissance sans limite
		for k, b := range l.hits {
			if now.After(b.reset) {
				delete(l.hits, k)
			}
		}
	}
	b, ok := l.hits[key]
	if !ok || now.After(b.reset) {
		l.hits[key] = &bucket{count: 1, reset: now.Add(l.window)}
		return true
	}
	b.count++
	return b.count <= l.max
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- JSON ----------

type errBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError traduit une erreur métier en réponse HTTP. Les erreurs
// inattendues sont journalisées mais jamais détaillées au client.
func writeError(w http.ResponseWriter, log *slog.Logger, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusUnprocessableEntity, errBody{Error: ve.Message, Field: ve.Field})
	case errors.Is(err, domain.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, errBody{Error: "authentification requise ou identifiants invalides"})
	case errors.Is(err, domain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errBody{Error: "action non autorisée"})
	case errors.Is(err, domain.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{Error: "ressource introuvable"})
	case errors.Is(err, domain.ErrConflict):
		writeJSON(w, http.StatusConflict, errBody{Error: "cette ressource existe déjà"})
	case errors.Is(err, domain.ErrInvalidTransition):
		writeJSON(w, http.StatusConflict, errBody{Error: err.Error()})
	default:
		log.Error("erreur interne", "err", err)
		writeJSON(w, http.StatusInternalServerError, errBody{Error: "erreur interne"})
	}
}

// decode lit un corps JSON strict (champs inconnus refusés, taille limitée).
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &domain.ValidationError{Field: "body", Message: "corps trop volumineux"}
		}
		return &domain.ValidationError{Field: "body", Message: "JSON invalide"}
	}
	if _, err := dec.Token(); err != io.EOF {
		return &domain.ValidationError{Field: "body", Message: "JSON invalide : contenu après l'objet"}
	}
	return nil
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, &domain.ValidationError{Field: "id", Message: "identifiant invalide"}
	}
	return id, nil
}

// ---------- Handlers : authentification et utilisateurs ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, errBody{Error: "trop de tentatives, réessayez dans une minute"})
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	res, err := s.svc.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, a service.Actor) {
	u, err := s.svc.Me(r.Context(), a)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request, a service.Actor) {
	users, err := s.svc.ListUsers(r.Context(), a)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request, a service.Actor) {
	var in service.UserInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	u, err := s.svc.CreateUser(r.Context(), a, in)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

// ---------- Handlers : tickets ----------

func (s *Server) handleClassify(w http.ResponseWriter, r *http.Request, _ service.Actor) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	sug, err := s.svc.Classify(r.Context(), in.Title, in.Description)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, sug)
}

func intParam(r *http.Request, name string) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, &domain.ValidationError{Field: name, Message: "entier positif attendu"}
	}
	return n, nil
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request, a service.Actor) {
	q := r.URL.Query()
	f := service.TicketFilter{Query: q.Get("q")}
	if v := q.Get("status"); v != "" {
		st := domain.Status(v)
		if !st.Valid() {
			writeError(w, s.log, &domain.ValidationError{Field: "status", Message: "statut inconnu"})
			return
		}
		f.Status = &st
	}
	if v := q.Get("category"); v != "" {
		c := domain.Category(v)
		if !c.Valid() {
			writeError(w, s.log, &domain.ValidationError{Field: "category", Message: "catégorie inconnue"})
			return
		}
		f.Category = &c
	}
	if q.Get("assignee") == "me" {
		id := a.ID
		f.AssigneeID = &id
	}
	if v := q.Get("equipment_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, s.log, &domain.ValidationError{Field: "equipment_id", Message: "identifiant invalide"})
			return
		}
		f.EquipmentID = &id
	}
	var err error
	if f.Limit, err = intParam(r, "limit"); err != nil {
		writeError(w, s.log, err)
		return
	}
	if f.Offset, err = intParam(r, "offset"); err != nil {
		writeError(w, s.log, err)
		return
	}
	list, err := s.svc.ListTickets(r.Context(), a, f)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request, a service.Actor) {
	var in service.TicketInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	t, err := s.svc.CreateTicket(r.Context(), a, in)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request, a service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	d, err := s.svc.GetTicket(r.Context(), a, id)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request, a service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	var p service.TicketPatch
	if err := decode(w, r, &p); err != nil {
		writeError(w, s.log, err)
		return
	}
	t, err := s.svc.UpdateTicket(r.Context(), a, id, p)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleChangeStatus(w http.ResponseWriter, r *http.Request, a service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	var in struct {
		Status domain.Status `json:"status"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	t, err := s.svc.ChangeStatus(r.Context(), a, id, in.Status)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request, a service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	c, err := s.svc.AddComment(r.Context(), a, id, in.Body)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// ---------- Handlers : équipements et statistiques ----------

func (s *Server) handleListEquipment(w http.ResponseWriter, r *http.Request, _ service.Actor) {
	q := r.URL.Query()
	f := service.EquipmentFilter{Query: q.Get("q")}
	if v := q.Get("type"); v != "" {
		t := domain.EquipmentType(v)
		if !t.Valid() {
			writeError(w, s.log, &domain.ValidationError{Field: "type", Message: "type inconnu"})
			return
		}
		f.Type = &t
	}
	if v := q.Get("status"); v != "" {
		st := domain.EquipmentStatus(v)
		if !st.Valid() {
			writeError(w, s.log, &domain.ValidationError{Field: "status", Message: "statut inconnu"})
			return
		}
		f.Status = &st
	}
	list, err := s.svc.ListEquipment(r.Context(), f)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateEquipment(w http.ResponseWriter, r *http.Request, a service.Actor) {
	var in service.EquipmentInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	e, err := s.svc.CreateEquipment(r.Context(), a, in)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) handleGetEquipment(w http.ResponseWriter, r *http.Request, _ service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	e, err := s.svc.GetEquipment(r.Context(), id)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleUpdateEquipment(w http.ResponseWriter, r *http.Request, a service.Actor) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	var in service.EquipmentInput
	if err := decode(w, r, &in); err != nil {
		writeError(w, s.log, err)
		return
	}
	e, err := s.svc.UpdateEquipment(r.Context(), a, id, in)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request, a service.Actor) {
	st, err := s.svc.Stats(r.Context(), a)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleAIInsights(w http.ResponseWriter, r *http.Request, a service.Actor) {
	insights, err := s.svc.AIInsights(r.Context(), a)
	if err != nil {
		writeError(w, s.log, err)
		return
	}
	writeJSON(w, http.StatusOK, insights)
}
