// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonSaltLen    = 16
	argonKeyLen     = 32
	argonTime       = 3
	argonMemory     = 64 * 1024
	argonThreads    = 2
	passwordMinLen  = 12
	passwordMaxLen  = 256
	argonVersionStr = "19"
)

// HashPassword returns a PHC-formatted Argon2id hash of password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%s$m=%d,t=%d,p=%d$%s$%s",
		argonVersionStr, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encoded. Malformed hashes
// return false rather than an error.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return false
	}

	var memory, timeCost uint32
	var threads uint8
	for _, field := range strings.Split(parts[3], ",") {
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false
		}
		switch name {
		case "m":
			memory = uint32(n)
		case "t":
			timeCost = uint32(n)
		case "p":
			if n > 255 {
				return false
			}
			threads = uint8(n)
		}
	}
	// Reject malformed or hostile parameters instead of allocating wildly.
	if memory == 0 || timeCost == 0 || threads == 0 || memory > 1<<20 || timeCost > 10 || threads > 16 {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != argonSaltLen {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != argonKeyLen {
		return false
	}

	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, argonKeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword enforces the minimum and maximum password lengths.
func ValidatePassword(password string) error {
	if len(password) < passwordMinLen {
		return fmt.Errorf("password must be at least %d characters", passwordMinLen)
	}
	if len(password) > passwordMaxLen {
		return fmt.Errorf("password must be at most %d characters", passwordMaxLen)
	}
	return nil
}
