// Package auth fournit le hachage des mots de passe (PBKDF2-HMAC-SHA256,
// bibliothèque standard Go ≥ 1.24) et des jetons JWT HS256 signés.
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------- Mots de passe ----------

// PBKDF2Iterations suit la recommandation OWASP pour PBKDF2-HMAC-SHA256.
// C'est une variable uniquement pour que les tests puissent l'abaisser ;
// ne la modifiez pas en production. Le nombre d'itérations est stocké dans
// chaque hash, donc les anciens hash restent valides si la valeur change.
var PBKDF2Iterations = 600_000

const (
	saltLen = 16
	keyLen  = 32
)

// MinPasswordLength est la longueur minimale acceptée pour un mot de passe.
const MinPasswordLength = 8

// HashPassword renvoie un hash au format "pbkdf2-sha256$iter$sel$clé" (base64).
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, PBKDF2Iterations, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		PBKDF2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword compare un mot de passe à un hash en temps constant.
func CheckPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---------- JWT HS256 ----------

var (
	ErrInvalidToken = errors.New("jeton invalide")
	ErrExpiredToken = errors.New("jeton expiré")
)

// Claims est le contenu du jeton.
type Claims struct {
	Subject int64  `json:"sub"`
	Role    string `json:"role"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

var b64 = base64.RawURLEncoding

const jwtHeader = `{"alg":"HS256","typ":"JWT"}`

// TokenManager émet et vérifie les jetons.
type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenManager exige un secret d'au moins 32 octets.
func NewTokenManager(secret string, ttl time.Duration) (*TokenManager, error) {
	if len(secret) < 32 {
		return nil, errors.New("JWT_SECRET doit contenir au moins 32 caractères")
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

func (m *TokenManager) sign(signingInput string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

// Issue crée un jeton signé pour l'utilisateur.
func (m *TokenManager) Issue(userID int64, role string) (string, time.Time, error) {
	now := m.now()
	exp := now.Add(m.ttl)
	payload, err := json.Marshal(Claims{Subject: userID, Role: role, Issued: now.Unix(), Expires: exp.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}
	signingInput := b64.EncodeToString([]byte(jwtHeader)) + "." + b64.EncodeToString(payload)
	return signingInput + "." + b64.EncodeToString(m.sign(signingInput)), exp, nil
}

// Parse vérifie signature, algorithme et expiration.
func (m *TokenManager) Parse(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	headerRaw, err := b64.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Alg != "HS256" {
		return nil, ErrInvalidToken // refuse "none" et tout autre algorithme
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, ErrInvalidToken
	}
	if !hmac.Equal(sig, m.sign(parts[0]+"."+parts[1])) {
		return nil, ErrInvalidToken
	}
	payload, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.Subject <= 0 {
		return nil, ErrInvalidToken
	}
	if m.now().Unix() >= c.Expires {
		return nil, ErrExpiredToken
	}
	return &c, nil
}
