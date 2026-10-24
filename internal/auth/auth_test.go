package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("Motdepasse!2026")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("Motdepasse!2026", h) {
		t.Fatal("le bon mot de passe doit être accepté")
	}
	if CheckPassword("autre", h) {
		t.Fatal("un mauvais mot de passe doit être refusé")
	}
	h2, _ := HashPassword("Motdepasse!2026")
	if h == h2 {
		t.Fatal("deux hash du même mot de passe doivent différer (sel aléatoire)")
	}
	for _, bad := range []string{"", "x", "a$b$c$d", "pbkdf2-sha256$abc$AA$AA", "pbkdf2-sha256$1$%%%$AA"} {
		if CheckPassword("x", bad) {
			t.Fatalf("hash malformé accepté : %q", bad)
		}
	}
}

func TestTokenRoundTrip(t *testing.T) {
	m, err := NewTokenManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tok, exp, err := m.Issue(42, "technician")
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) <= 0 {
		t.Fatal("expiration dans le passé")
	}
	c, err := m.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != 42 || c.Role != "technician" {
		t.Fatalf("claims inattendus : %+v", c)
	}
}

func TestTokenRejections(t *testing.T) {
	m, _ := NewTokenManager(testSecret, time.Hour)
	tok, _, _ := m.Issue(1, "admin")
	parts := strings.Split(tok, ".")

	// signature modifiée
	if _, err := m.Parse(parts[0] + "." + parts[1] + "." + "AAAA"); err != ErrInvalidToken {
		t.Errorf("signature falsifiée : %v", err)
	}
	// charge utile modifiée (élévation de rôle) avec l'ancienne signature
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":1,"role":"admin","iat":0,"exp":99999999999}`))
	if _, err := m.Parse(parts[0] + "." + forged + "." + parts[2]); err != ErrInvalidToken {
		t.Errorf("charge falsifiée : %v", err)
	}
	// alg=none
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	if _, err := m.Parse(none + "." + parts[1] + "."); err != ErrInvalidToken {
		t.Errorf("alg none : %v", err)
	}
	// autre secret
	other, _ := NewTokenManager("fedcba9876543210fedcba9876543210", time.Hour)
	if _, err := other.Parse(tok); err != ErrInvalidToken {
		t.Errorf("autre secret : %v", err)
	}
	// format
	for _, bad := range []string{"", "a.b", "a.b.c.d", "!!!.!!!.!!!"} {
		if _, err := m.Parse(bad); err != ErrInvalidToken {
			t.Errorf("format %q : %v", bad, err)
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	m, _ := NewTokenManager(testSecret, time.Minute)
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return base }
	tok, _, _ := m.Issue(5, "employee")
	m.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := m.Parse(tok); err != ErrExpiredToken {
		t.Fatalf("ErrExpiredToken attendu, obtenu %v", err)
	}
}

func TestShortSecretRejected(t *testing.T) {
	if _, err := NewTokenManager("court", time.Hour); err == nil {
		t.Fatal("un secret court doit être refusé")
	}
}
