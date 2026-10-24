package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/meriem/setram-helpdesk/internal/domain"
)

func TestKeywordClassifier(t *testing.T) {
	cases := []struct {
		name, title, desc string
		cat               domain.Category
		prio              domain.Priority
	}{
		{"billetterie fr", "Guichet bloqué", "Le distributeur de billets ne fonctionne pas", domain.CategoryTicketing, domain.PriorityCritical},
		{"imprimante fr", "Imprimante", "Bourrage papier, l'imprimante est en panne", domain.CategoryPrinter, domain.PriorityHigh},
		{"réseau ar", "انقطاع الشبكة", "لا يوجد اتصال بالإنترنت في المكتب", domain.CategoryNetwork, domain.PriorityMedium},
		{"شباك ar", "عطل في شباك التذاكر", "الجهاز لا يعمل", domain.CategoryTicketing, domain.PriorityHigh},
		{"طابعة ar", "الطابعة", "نفد الحبر", domain.CategoryPrinter, domain.PriorityMedium},
		{"pc fr", "Lenteur", "Mon pc démarre très lentement", domain.CategoryComputer, domain.PriorityMedium},
		{"autre", "Demande", "Besoin d'une chaise", domain.CategoryOther, domain.PriorityMedium},
		{"faible", "Écran", "Question : amélioration de l'écran, pas urgent", domain.CategoryComputer, domain.PriorityLow},
	}
	var k KeywordClassifier
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := k.Classify(context.Background(), c.title, c.desc)
			if err != nil {
				t.Fatal(err)
			}
			if s.Category != c.cat {
				t.Errorf("catégorie %s, attendu %s", s.Category, c.cat)
			}
			if s.Priority != c.prio {
				t.Errorf("priorité %s, attendu %s", s.Priority, c.prio)
			}
			if s.Source != SourceKeywords || s.Summary == "" {
				t.Errorf("source/résumé inattendus : %+v", s)
			}
		})
	}
}

func TestKeywordSummaryTruncated(t *testing.T) {
	s, _ := KeywordClassifier{}.Classify(context.Background(), strings.Repeat("é", 1000), "")
	if n := len([]rune(s.Summary)); n != maxSummaryRunes {
		t.Fatalf("résumé de %d runes, attendu %d", n, maxSummaryRunes)
	}
}

func fakeAnthropic(t *testing.T, status int, text string, check func(*http.Request, map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if check != nil {
			check(r, body)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		})
	}))
}

func TestAnthropicClassifierOK(t *testing.T) {
	srv := fakeAnthropic(t, 200, "Voici : {\"category\":\"network\",\"priority\":\"high\",\"summary\":\"  Coupure   réseau au dépôt \"}",
		func(r *http.Request, body map[string]any) {
			if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
				t.Errorf("requête inattendue : %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") != "2023-06-01" {
				t.Errorf("en-têtes manquants : %v", r.Header)
			}
			if body["model"] != "m" {
				t.Errorf("modèle %v", body["model"])
			}
		})
	defer srv.Close()

	a := &AnthropicClassifier{APIKey: "k", Model: "m", BaseURL: srv.URL}
	s, err := a.Classify(context.Background(), "Réseau", "plus de connexion")
	if err != nil {
		t.Fatal(err)
	}
	if s.Category != domain.CategoryNetwork || s.Priority != domain.PriorityHigh || s.Source != SourceAI {
		t.Fatalf("suggestion inattendue : %+v", s)
	}
	if s.Summary != "Coupure réseau au dépôt" {
		t.Fatalf("résumé non normalisé : %q", s.Summary)
	}
}

func TestAnthropicClassifierRejectsBadModelOutput(t *testing.T) {
	bad := []string{
		`{"category":"hacked","priority":"high","summary":"x"}`,
		`{"category":"network","priority":"urgent!!","summary":"x"}`,
		`pas de json ici`,
		`{"category":`,
	}
	for _, text := range bad {
		srv := fakeAnthropic(t, 200, text, nil)
		a := &AnthropicClassifier{APIKey: "k", Model: "m", BaseURL: srv.URL}
		if _, err := a.Classify(context.Background(), "t", "d"); err == nil {
			t.Errorf("sortie invalide acceptée : %q", text)
		}
		srv.Close()
	}
}

func TestAnthropicClassifierHTTPErrorAndNoKey(t *testing.T) {
	srv := fakeAnthropic(t, 500, "", nil)
	defer srv.Close()
	a := &AnthropicClassifier{APIKey: "k", Model: "m", BaseURL: srv.URL}
	if _, err := a.Classify(context.Background(), "t", "d"); err == nil {
		t.Fatal("erreur HTTP attendue")
	}
	if _, err := (&AnthropicClassifier{}).Classify(context.Background(), "t", "d"); err == nil {
		t.Fatal("clé absente : erreur attendue")
	}
}

func TestAnthropicClassifierTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()
	a := &AnthropicClassifier{APIKey: "k", Model: "m", BaseURL: srv.URL, Timeout: 50 * time.Millisecond}
	if _, err := a.Classify(context.Background(), "t", "d"); err == nil {
		t.Fatal("timeout attendu")
	}
}

type failing struct{}

func (failing) Classify(context.Context, string, string) (*Suggestion, error) {
	return nil, errors.New("panne IA")
}

func TestFallbackClassifier(t *testing.T) {
	f := FallbackClassifier{Primary: failing{}, Fallback: KeywordClassifier{}}
	s, err := f.Classify(context.Background(), "Imprimante", "en panne")
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != SourceKeywords || s.Category != domain.CategoryPrinter {
		t.Fatalf("repli inattendu : %+v", s)
	}
	// Sans primaire : repli direct
	if _, err := (FallbackClassifier{Fallback: KeywordClassifier{}}).Classify(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
}
