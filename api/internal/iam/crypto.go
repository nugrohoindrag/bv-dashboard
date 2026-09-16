package iam

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

// ---------- Password (argon2id, parameter OWASP) ----------

const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---------- Refresh token (opaque 256-bit, disimpan hash) ----------

func NewRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ---------- JWT (EdDSA / Ed25519) ----------

type Claims struct {
	jwt.RegisteredClaims
	Org string `json:"org"`
	Sid string `json:"sid"`
	Ver int    `json:"ver"`
	Src string `json:"src"` // web | mobile
}

type TokenSigner struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	ttl  time.Duration
}

// NewTokenSigner memuat Ed25519 dari PEM (PKCS8) atau generate ephemeral bila kosong (local).
func NewTokenSigner(privPEM, pubPEM string, ttl time.Duration) (*TokenSigner, error) {
	if privPEM == "" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		return &TokenSigner{priv: priv, pub: pub, ttl: ttl}, nil
	}
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, errors.New("invalid private key PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not Ed25519")
	}
	return &TokenSigner{priv: priv, pub: priv.Public().(ed25519.PublicKey), ttl: ttl}, nil
}

func (s *TokenSigner) TTL() time.Duration { return s.ttl }

// Keys mengekspos pasangan kunci untuk signer lain yang memakai issuer berbeda (token customer BVRooms);
// token tersebut tidak pernah diterima Authenticate karena issuer diverifikasi.
func (s *TokenSigner) Keys() (ed25519.PrivateKey, ed25519.PublicKey) { return s.priv, s.pub }

func (s *TokenSigner) Sign(userID, orgID, sessionID uuid.UUID, ver int, src string, now time.Time) (string, time.Time, error) {
	exp := now.Add(s.ttl)
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    "buildingvision",
			ID:        uuid.NewString(),
		},
		Org: orgID.String(), Sid: sessionID.String(), Ver: ver, Src: src,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := tok.SignedString(s.priv)
	return signed, exp, err
}

func (s *TokenSigner) Verify(token string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.pub, nil
	}, jwt.WithIssuer("buildingvision"), jwt.WithLeeway(30*time.Second))
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// ExportKeysPEM: untuk `bvctl keygen`.
func ExportKeysPEM() (privPEM, pubPEM string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	pb, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	ub, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pb})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: ub})), nil
}
