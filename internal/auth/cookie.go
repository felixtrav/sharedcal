package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// sealer encrypts and authenticates small cookie payloads (AES-256-GCM with
// a key derived from SESSION_SECRET). The OIDC flow cookie holds the PKCE
// verifier, so it must be confidential as well as tamper-proof.
type sealer struct {
	aead cipher.AEAD
}

func newSealer(secret []byte, purpose string) (*sealer, error) {
	key, err := hkdf.Key(sha256.New, secret, nil, "sharedcal:"+purpose, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &sealer{aead: aead}, nil
}

type sealed[T any] struct {
	Exp  int64 `json:"exp"`
	Data T     `json:"d"`
}

var errBadCookie = errors.New("invalid or expired cookie")

func seal[T any](s *sealer, v T, ttl time.Duration) (string, error) {
	plain, err := json.Marshal(sealed[T]{Exp: time.Now().Add(ttl).Unix(), Data: v})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, plain, nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

func open[T any](s *sealer, value string) (T, error) {
	var zero T
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return zero, errBadCookie
	}
	nonce, ct := raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():]
	plain, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return zero, errBadCookie
	}
	var v sealed[T]
	if err := json.Unmarshal(plain, &v); err != nil {
		return zero, errBadCookie
	}
	if time.Now().Unix() > v.Exp {
		return zero, errBadCookie
	}
	return v.Data, nil
}

// randomToken returns n random bytes, base64url-encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns the hex SHA-256 of a secret token, which is what the
// database stores for sessions and feed tokens.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func macEqual(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
