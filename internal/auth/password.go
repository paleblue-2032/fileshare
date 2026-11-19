package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	hashScheme     = "pbkdf2-sha256"
	hashIterations = 600_000
	hashKeyLen     = 32
	saltLen        = 16
	minIterations  = 100_000
	maxIterations  = 5_000_000
	maxPasswordLen = 4096
)

var (
	ErrWeakPassword = errors.New("password must be at least 8 characters")
	ErrBadHash      = errors.New("invalid password hash")
)

// HashPassword derives a PBKDF2-SHA256 hash. The encoded form uses ':' so it
// can be stored in a systemd EnvironmentFile without shell expansion.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", ErrWeakPassword
	}
	if len(password) > maxPasswordLen {
		return "", errors.New("password is too long")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, hashIterations, hashKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d:%s:%s", hashScheme, hashIterations, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) bool {
	iter, salt, want, err := parseHash(encoded)
	if err != nil || len(password) > maxPasswordLen {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func ValidHash(encoded string) bool {
	_, _, _, err := parseHash(encoded)
	return err == nil
}

func parseHash(encoded string) (int, []byte, []byte, error) {
	parts := strings.Split(encoded, ":")
	if len(parts) != 4 || parts[0] != hashScheme {
		return 0, nil, nil, ErrBadHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < minIterations || iter > maxIterations {
		return 0, nil, nil, ErrBadHash
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return 0, nil, nil, ErrBadHash
	}
	key, err := hex.DecodeString(parts[3])
	if err != nil || len(key) < 16 || len(key) > 128 {
		return 0, nil, nil, ErrBadHash
	}
	return iter, salt, key, nil
}

// Digest is a fast cache key for an already-verified password. It must never
// be used as password storage.
func Digest(password string) [32]byte {
	return sha256.Sum256([]byte(password))
}
