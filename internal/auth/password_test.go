// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	const password = "correct horse battery staple"

	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(first, password) {
		t.Fatal("VerifyPassword rejected the original password")
	}
	if VerifyPassword(first, "wrong password") {
		t.Fatal("VerifyPassword accepted a wrong password")
	}

	parts := strings.Split(first, "$")
	if len(parts) != 6 {
		t.Fatalf("unexpected hash format %q", first)
	}
	replacement := byte('A')
	if parts[5][0] == 'A' {
		replacement = 'B'
	}
	tampered := strings.Join([]string{parts[0], parts[1], parts[2], parts[3], parts[4], string(replacement) + parts[5][1:]}, "$")
	if VerifyPassword(tampered, password) {
		t.Fatal("VerifyPassword accepted a tampered hash")
	}

	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical; salt is not random")
	}
	if VerifyPassword("not a hash", password) {
		t.Fatal("VerifyPassword accepted a malformed hash")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword(strings.Repeat("a", 11)); err == nil {
		t.Fatal("11-character password passed validation")
	}
	if err := ValidatePassword(strings.Repeat("a", 12)); err != nil {
		t.Fatalf("12-character password failed validation: %v", err)
	}
}
