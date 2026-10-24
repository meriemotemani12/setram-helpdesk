package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

const secret = "0123456789abcdef0123456789abcdef"

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": secret}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.Store != "postgres" || !c.AutoMigrate || c.SeedDemo || c.TokenTTL != 12*time.Hour {
		t.Errorf("valeurs par défaut inattendues : %+v", c)
	}
	if c.AnthropicModel == "" || c.AnthropicAPIKey != "" {
		t.Errorf("IA : %+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"sans base", map[string]string{"JWT_SECRET": secret}, "DATABASE_URL"},
		{"secret court", map[string]string{"DATABASE_URL": "x", "JWT_SECRET": "court"}, "JWT_SECRET"},
		{"store inconnu", map[string]string{"STORE": "mongo", "JWT_SECRET": secret}, "STORE"},
		{"ttl invalide", map[string]string{"STORE": "memory", "JWT_SECRET": secret, "TOKEN_TTL": "abc"}, "TOKEN_TTL"},
		{"bool invalide", map[string]string{"STORE": "memory", "JWT_SECRET": secret, "AUTO_MIGRATE": "peut-être"}, "AUTO_MIGRATE"},
		{"admin incomplet", map[string]string{"STORE": "memory", "JWT_SECRET": secret, "ADMIN_EMAIL": "a@b.dz"}, "ADMIN_EMAIL"},
		{"démo sans mot de passe", map[string]string{"STORE": "memory", "JWT_SECRET": secret, "SEED_DEMO": "true"}, "DEMO_PASSWORD"},
	}
	for _, c := range cases {
		_, err := Load(env(c.env))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s : erreur contenant %q attendue, obtenu %v", c.name, c.want, err)
		}
	}
}

func TestLoadMemoryAndAdmin(t *testing.T) {
	c, err := Load(env(map[string]string{
		"STORE": "memory", "JWT_SECRET": secret, "ADMIN_EMAIL": "a@b.dz", "ADMIN_PASSWORD": "motdepasse-admin",
		"SEED_DEMO": "true", "DEMO_PASSWORD": "demo-password", "TOKEN_TTL": "30m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Store != "memory" || c.AdminEmail != "a@b.dz" || !c.SeedDemo || c.TokenTTL != 30*time.Minute {
		t.Errorf("%+v", c)
	}
}
