// Package auth holds password hashing and session token helpers.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: 7 MiB / t=5 / p=1, one of OWASP's equivalent recommended
// settings. It trades memory for iterations so logins fit the 40 MB RSS budget.
const (
	argonMemory  = 7 * 1024 // KiB
	argonTime    = 5
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLen is the shortest accepted password.
const MinPasswordLen = 10

// ErrMismatch is returned when a password does not match its hash.
var ErrMismatch = errors.New("password does not match")

var b64 = base64.RawStdEncoding

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(pw string) (string, error) {
	if len(pw) < MinPasswordLen {
		return "", fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := idKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a hash made by HashPassword.
func VerifyPassword(hash, pw string) error {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("unsupported password hash")
	}
	var v int
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return fmt.Errorf("parse argon2 params: %w", err)
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("decode key: %w", err)
	}
	got := idKey([]byte(pw), salt, t, m, p, uint32(len(want))) // #nosec G115 -- key length is 32
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// hashMu lets one argon2 computation run at a time, so concurrent logins cannot
// multiply its memory use (and brute force is naturally throttled).
var hashMu sync.Mutex

func idKey(pw, salt []byte, t, m uint32, p uint8, keyLen uint32) []byte {
	hashMu.Lock()
	defer hashMu.Unlock()
	return argon2.IDKey(pw, salt, t, m, p, keyLen)
}

var dummySalt = make([]byte, saltLen)

// BurnTime spends the same effort as a real password check. It is used when a
// username does not exist, so unknown users take as long as wrong passwords.
func BurnTime(pw string) {
	_ = idKey([]byte(pw), dummySalt, argonTime, argonMemory, argonThreads, argonKeyLen)
}

// NewToken returns a random URL-safe token (256 bits).
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken is how session tokens are stored: the DB never holds the cookie value.
func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// Equal compares two secrets in constant time.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
