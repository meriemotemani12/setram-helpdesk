package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/meriem/setram-helpdesk/internal/auth"
	"github.com/meriem/setram-helpdesk/internal/domain"
	"github.com/meriem/setram-helpdesk/internal/httpapi"
	"github.com/meriem/setram-helpdesk/internal/service"
	"github.com/meriem/setram-helpdesk/internal/store/memory"
)

type testServer struct {
	srv                     *httptest.Server
	admin, tech, emp1, emp2 string // jetons
}

func newTestServer(t *testing.T, rateLimit int) *testServer {
	t.Helper()
	auth.PBKDF2Iterations = 1000
	tm, err := auth.NewTokenManager("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(memory.New(), tm, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnsureAdmin(t.Context(), "admin@setram.dz", "motdepasse-admin", "Admin"); err != nil {
		t.Fatal(err)
	}
	admin := service.Actor{ID: 1, Role: domain.RoleAdmin}
	for _, u := range []service.UserInput{
		{Email: "tech@setram.dz", FullName: "Technicien", Role: domain.RoleTechnician, Password: "motdepasse-ok"},
		{Email: "emp1@setram.dz", FullName: "Employé Un", Role: domain.RoleEmployee, Password: "motdepasse-ok"},
		{Email: "emp2@setram.dz", FullName: "Employé Deux", Role: domain.RoleEmployee, Password: "motdepasse-ok"},
	} {
		if _, err := svc.CreateUser(t.Context(), admin, u); err != nil {
			t.Fatal(err)
		}
	}
	static := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>SETRAM</title>")},
		"app.js":     {Data: []byte("console.log(1)")},
	}
	h := httpapi.New(svc, httpapi.Options{
		Static: static, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		LoginRateLimit: rateLimit, LoginWindow: time.Minute,
	})
	ts := &testServer{srv: httptest.NewServer(h.Handler())}
	t.Cleanup(ts.srv.Close)
	ts.admin = ts.login(t, "admin@setram.dz", "motdepasse-admin")
	ts.tech = ts.login(t, "tech@setram.dz", "motdepasse-ok")
	ts.emp1 = ts.login(t, "emp1@setram.dz", "motdepasse-ok")
	ts.emp2 = ts.login(t, "emp2@setram.dz", "motdepasse-ok")
	return ts
}

func (ts *testServer) do(t *testing.T, method, path, token string, body any) (int, map[string]any, []byte) {
	t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rdr = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rdr = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, ts.srv.URL+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rdr != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, m, raw
}

func (ts *testServer) login(t *testing.T, email, pw string) string {
	t.Helper()
	code, m, _ := ts.do(t, "POST", "/api/auth/login", "", map[string]string{"email": email, "password": pw})
	if code != 200 {
		t.Fatalf("login %s : %d %v", email, code, m)
	}
	return m["token"].(string)
}

func TestAuthRequiredAndHeaders(t *testing.T) {
	ts := newTestServer(t, 100)
	for _, p := range []string{"/api/me", "/api/tickets", "/api/equipment", "/api/stats", "/api/users"} {
		if code, _, _ := ts.do(t, "GET", p, "", nil); code != 401 {
			t.Errorf("GET %s sans jeton : %d", p, code)
		}
		if code, _, _ := ts.do(t, "GET", p, "jeton.bidon.invalide", nil); code != 401 {
			t.Errorf("GET %s jeton invalide : %d", p, code)
		}
	}
	resp, err := http.Get(ts.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz : %d", resp.StatusCode)
	}
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy", "Referrer-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("en-tête de sécurité manquant : %s", h)
		}
	}
	if code, _, _ := ts.do(t, "GET", "/api/inconnue", ts.tech, nil); code != 404 {
		t.Errorf("route inconnue : %d", code)
	}
}

func TestStaticServing(t *testing.T) {
	ts := newTestServer(t, 100)
	for path, want := range map[string]string{"/": "SETRAM", "/app.js": "console.log"} {
		resp, err := http.Get(ts.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), want) {
			t.Errorf("GET %s : %d %q", path, resp.StatusCode, b)
		}
	}
}

func TestLoginRateLimitAndNoUserEnumeration(t *testing.T) {
	ts2 := newTestServer(t, 1000)
	_, m1, _ := ts2.do(t, "POST", "/api/auth/login", "", map[string]string{"email": "tech@setram.dz", "password": "faux"})
	_, m2, _ := ts2.do(t, "POST", "/api/auth/login", "", map[string]string{"email": "nobody@setram.dz", "password": "faux"})
	if m1["error"] != m2["error"] {
		t.Errorf("les messages d'erreur doivent être identiques : %v / %v", m1, m2)
	}

	limited := newTestServerNoLogin(t, 3)
	var last int
	for i := 0; i < 5; i++ {
		last, _, _ = limited.do(t, "POST", "/api/auth/login", "", map[string]string{"email": "x@setram.dz", "password": "faux"})
		if i < 3 && last != 401 {
			t.Fatalf("tentative %d : %d", i, last)
		}
	}
	if last != 429 {
		t.Fatalf("la 5e tentative doit être limitée (429), obtenu %d", last)
	}
}

// newTestServerNoLogin démarre un serveur sans effectuer de connexion préalable.
func newTestServerNoLogin(t *testing.T, rateLimit int) *testServer {
	t.Helper()
	auth.PBKDF2Iterations = 1000
	tm, _ := auth.NewTokenManager("0123456789abcdef0123456789abcdef", time.Hour)
	svc, _ := service.New(memory.New(), tm, nil)
	h := httpapi.New(svc, httpapi.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), LoginRateLimit: rateLimit})
	ts := &testServer{srv: httptest.NewServer(h.Handler())}
	t.Cleanup(ts.srv.Close)
	return ts
}

func TestTicketLifecycleOverHTTP(t *testing.T) {
	ts := newTestServer(t, 100)

	// équipement (technicien)
	code, eq, _ := ts.do(t, "POST", "/api/equipment", ts.tech, map[string]any{
		"asset_tag": "IMP-001", "name": "Imprimante dépôt", "type": "printer", "location": "Dépôt",
		"next_maintenance_at": "2026-12-01T00:00:00Z",
	})
	if code != 201 {
		t.Fatalf("création équipement : %d %v", code, eq)
	}
	eqID := eq["id"]

	// l'employé signale une panne
	code, tk, _ := ts.do(t, "POST", "/api/tickets", ts.emp1, map[string]any{
		"title": "Imprimante en panne", "description": "Bourrage papier dans l'imprimante du dépôt", "equipment_id": eqID,
	})
	if code != 201 {
		t.Fatalf("création ticket : %d %v", code, tk)
	}
	id := int(tk["id"].(float64))
	path := "/api/tickets/" + itoa(id)
	if tk["status"] != "new" || tk["category"] != "printer" {
		t.Errorf("ticket : %v", tk)
	}

	// l'autre employé ne le voit pas
	if code, _, _ := ts.do(t, "GET", path, ts.emp2, nil); code != 404 {
		t.Errorf("ticket d'autrui : %d", code)
	}
	// l'employé ne peut pas changer le statut
	if code, _, _ := ts.do(t, "POST", path+"/status", ts.emp1, map[string]string{"status": "in_progress"}); code != 403 {
		t.Errorf("employé change statut : %d", code)
	}
	// transition invalide
	if code, _, _ := ts.do(t, "POST", path+"/status", ts.tech, map[string]string{"status": "done"}); code != 409 {
		t.Errorf("new → done : %d", code)
	}
	// workflow normal
	for _, st := range []string{"in_progress", "done"} {
		if code, m, _ := ts.do(t, "POST", path+"/status", ts.tech, map[string]string{"status": st}); code != 200 || m["status"] != st {
			t.Fatalf("→ %s : %d %v", st, code, m)
		}
	}
	// commentaire par l'employé propriétaire
	if code, _, _ := ts.do(t, "POST", path+"/comments", ts.emp1, map[string]string{"body": "Merci !"}); code != 201 {
		t.Errorf("commentaire : %d", code)
	}
	code, detail, _ := ts.do(t, "GET", path, ts.emp1, nil)
	if code != 200 {
		t.Fatalf("détail : %d", code)
	}
	if n := len(detail["history"].([]any)); n != 3 {
		t.Errorf("historique : %d entrées", n)
	}
	if n := len(detail["comments"].([]any)); n != 1 {
		t.Errorf("commentaires : %d", n)
	}

	// listes
	if _, _, raw := ts.do(t, "GET", "/api/tickets?status=done", ts.tech, nil); !strings.Contains(string(raw), `"id":`+itoa(id)) {
		t.Errorf("filtre status=done : %s", raw)
	}
	if _, _, raw := ts.do(t, "GET", "/api/tickets?q=bourrage", ts.emp2, nil); strings.TrimSpace(string(raw)) != "[]" {
		t.Errorf("emp2 ne doit rien voir : %s", raw)
	}
	if code, _, _ := ts.do(t, "GET", "/api/tickets?status=bogus", ts.tech, nil); code != 422 {
		t.Errorf("statut inconnu : %d", code)
	}
	if code, _, _ := ts.do(t, "GET", "/api/tickets?limit=abc", ts.tech, nil); code != 422 {
		t.Errorf("limit invalide : %d", code)
	}

	// statistiques
	code, st, _ := ts.do(t, "GET", "/api/stats", ts.tech, nil)
	if code != 200 || st["total_tickets"].(float64) != 1 || st["resolved_count"].(float64) != 1 {
		t.Errorf("stats : %d %v", code, st)
	}
	if code, _, _ := ts.do(t, "GET", "/api/stats", ts.emp1, nil); code != 403 {
		t.Errorf("stats employé : %d", code)
	}
}

func TestInputValidationOverHTTP(t *testing.T) {
	ts := newTestServer(t, 100)
	cases := []struct {
		name, body string
	}{
		{"JSON cassé", `{"title":`},
		{"champ inconnu", `{"title":"Titre valide","description":"description valide","admin":true}`},
		{"contenu après l'objet", `{"title":"Titre valide","description":"description valide"} {}`},
		{"titre trop court", `{"title":"a","description":"description valide"}`},
	}
	for _, c := range cases {
		if code, m, _ := ts.do(t, "POST", "/api/tickets", ts.emp1, c.body); code != 422 {
			t.Errorf("%s : %d %v", c.name, code, m)
		}
	}
	big := `{"title":"Titre valide","description":"` + strings.Repeat("a", 2<<20) + `"}`
	if code, _, _ := ts.do(t, "POST", "/api/tickets", ts.emp1, big); code != 422 {
		t.Errorf("corps trop gros : %d", code)
	}
	if code, _, _ := ts.do(t, "GET", "/api/tickets/abc", ts.tech, nil); code != 422 {
		t.Errorf("id invalide : %d", code)
	}
	if code, _, _ := ts.do(t, "GET", "/api/tickets/999", ts.tech, nil); code != 404 {
		t.Errorf("ticket inconnu : %d", code)
	}
}

func TestUsersAndRolesOverHTTP(t *testing.T) {
	ts := newTestServer(t, 100)
	body := map[string]string{"email": "n@setram.dz", "full_name": "Nouveau", "role": "employee", "password": "motdepasse-ok"}
	if code, _, _ := ts.do(t, "POST", "/api/users", ts.tech, body); code != 403 {
		t.Errorf("technicien crée un utilisateur : %d", code)
	}
	code, u, raw := ts.do(t, "POST", "/api/users", ts.admin, body)
	if code != 201 || u["role"] != "employee" {
		t.Fatalf("création : %d %v", code, u)
	}
	if strings.Contains(string(raw), "pbkdf2") || strings.Contains(string(raw), "password") {
		t.Errorf("le hash fuit dans la réponse : %s", raw)
	}
	if code, _, _ := ts.do(t, "POST", "/api/users", ts.admin, body); code != 409 {
		t.Errorf("doublon : %d", code)
	}
	if code, _, _ := ts.do(t, "GET", "/api/users", ts.emp1, nil); code != 403 {
		t.Errorf("liste utilisateurs par employé : %d", code)
	}
	if code, me, _ := ts.do(t, "GET", "/api/me", ts.emp1, nil); code != 200 || me["email"] != "emp1@setram.dz" {
		t.Errorf("me : %d %v", code, me)
	}
}

func TestClassifyEndpoint(t *testing.T) {
	ts := newTestServer(t, 100)
	code, m, _ := ts.do(t, "POST", "/api/tickets/classify", ts.emp1, map[string]string{
		"title": "الطابعة", "description": "الطابعة لا تعمل ونفد الحبر",
	})
	if code != 200 || m["category"] != "printer" || m["source"] != "keywords" {
		t.Errorf("classify : %d %v", code, m)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
