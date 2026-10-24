// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// RFC 6238, Appendix B. The secret is the ASCII string 12345678901234567890.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, tc := range cases {
		got, err := TOTPCode(rfcSecret, time.Unix(tc.unix, 0))
		if err != nil {
			t.Fatalf("computing code for %d: %v", tc.unix, err)
		}
		if got != tc.want {
			t.Errorf("code at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestValidateTOTPWindow(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generating secret: %v", err)
	}
	at := time.Unix(30*100, 0)
	codeAt := func(step int64) string {
		code, err := TOTPCode(secret, time.Unix(step*30, 0))
		if err != nil {
			t.Fatalf("computing code for step %d: %v", step, err)
		}
		return code
	}

	for _, step := range []int64{99, 100, 101} {
		got, ok := ValidateTOTP(secret, codeAt(step), at)
		if !ok || got != step {
			t.Errorf("code for step %d was not accepted (matched %d, ok %v)", step, got, ok)
		}
	}
	for _, step := range []int64{98, 102} {
		if _, ok := ValidateTOTP(secret, codeAt(step), at); ok {
			t.Errorf("code for step %d was accepted outside the one-step window", step)
		}
	}
	if _, ok := ValidateTOTP(secret, "12345", at); ok {
		t.Error("a five-digit code was accepted")
	}
	if _, ok := ValidateTOTP(secret, "abcdef", at); ok {
		t.Error("a non-numeric code was accepted")
	}
}

func TestStepTrackerRejectsReplay(t *testing.T) {
	var tracker stepTracker
	if !tracker.accept("user", 100, 100) {
		t.Fatal("first use of a step was rejected")
	}
	if tracker.accept("user", 100, 100) {
		t.Fatal("the same step was accepted twice")
	}
	if tracker.accept("user", 99, 100) {
		t.Fatal("an older step was accepted after a newer one")
	}
	if !tracker.accept("user", 101, 101) {
		t.Fatal("the next step was rejected")
	}
}

func TestChallengeLimits(t *testing.T) {
	now := time.Unix(1000, 0)
	if challengeExpired(now.Add(time.Second), now) {
		t.Error("a challenge expiring in the future is expired")
	}
	if !challengeExpired(now, now) {
		t.Error("a challenge expiring now is not expired")
	}
	if attemptsExhausted(maxChallengeAttempts - 1) {
		t.Error("the last allowed attempt was treated as exhausted")
	}
	if !attemptsExhausted(maxChallengeAttempts) {
		t.Error("the fifth attempt was not treated as exhausted")
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	store, err := secrets.NewBuiltin(pool, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("creating secret store: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(pool, log, "http://localhost:8080", WithSecrets(store))
	ctx := context.Background()

	admin := Principal{OrgID: orgID, Role: RoleAdmin, Kind: ActorSystem, Label: "test"}
	user, err := svc.CreateUser(ctx, admin, "twofactor@example.com", "Two Factor", testPassword, RoleViewer)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	p := Principal{
		OrgID: orgID, UserID: user.ID, Role: RoleViewer,
		Kind: ActorUser, ActorID: user.ID, Label: user.Email,
	}

	secret, otpauthURL, err := svc.SetupTOTP(ctx, p)
	if err != nil {
		t.Fatalf("setting up TOTP: %v", err)
	}
	if !strings.HasPrefix(otpauthURL, "otpauth://totp/aicm:twofactor@example.com?secret=") {
		t.Errorf("otpauth URL = %q", otpauthURL)
	}
	code, err := TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("computing setup code: %v", err)
	}
	codes, err := svc.EnableTOTP(ctx, p, code)
	if err != nil {
		t.Fatalf("enabling TOTP: %v", err)
	}
	if len(codes) != recoveryCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), recoveryCount)
	}
	for _, recovery := range codes {
		if len(recovery) != recoveryLength {
			t.Fatalf("recovery code %q is not %d characters", recovery, recoveryLength)
		}
	}

	status, err := svc.GetTOTPStatus(ctx, p)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if !status.Enabled || status.RecoveryCodesLeft != recoveryCount {
		t.Fatalf("status after enable = %+v", status)
	}

	first, err := svc.Login(ctx, user.Email, testPassword, "test", netip.Addr{})
	if err != nil {
		t.Fatalf("password login: %v", err)
	}
	if !first.TOTPRequired() {
		t.Fatal("password login did not ask for a second factor")
	}
	if _, err := svc.LoginTOTP(ctx, first.Challenge, codes[0], "test", netip.Addr{}); err != nil {
		t.Fatalf("first recovery code login: %v", err)
	}

	second, err := svc.Login(ctx, user.Email, testPassword, "test", netip.Addr{})
	if err != nil {
		t.Fatalf("second password login: %v", err)
	}
	if _, err := svc.LoginTOTP(ctx, second.Challenge, codes[0], "test", netip.Addr{}); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reused recovery code returned %v, want ErrInvalidCode", err)
	}

	status, err = svc.GetTOTPStatus(ctx, p)
	if err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if status.RecoveryCodesLeft != recoveryCount-1 {
		t.Errorf("recovery codes left = %d, want %d", status.RecoveryCodesLeft, recoveryCount-1)
	}

	third, err := svc.Login(ctx, user.Email, testPassword, "test", netip.Addr{})
	if err != nil {
		t.Fatalf("third password login: %v", err)
	}
	if _, err := svc.LoginTOTP(ctx, third.Challenge, codes[1], "test", netip.Addr{}); err != nil {
		t.Fatalf("unused recovery code login: %v", err)
	}
}

func TestLoginOIDCLinksIdentity(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := NewService(pool, log, "http://localhost:8080")
	ctx := context.Background()

	admin := Principal{OrgID: orgID, Role: RoleAdmin, Kind: ActorSystem, Label: "test"}
	user, err := svc.CreateUser(ctx, admin, "sso@example.com", "SSO", testPassword, RoleViewer)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}

	session, err := svc.LoginOIDC(ctx, "https://idp.example", "subject-1", "SSO@example.com", "test", netip.Addr{})
	if err != nil {
		t.Fatalf("single sign-on login: %v", err)
	}
	if session.Principal.UserID != user.ID {
		t.Fatalf("session user = %s, want %s", session.Principal.UserID, user.ID)
	}
	linked, _, _, err := svc.Me(ctx, session.Principal)
	if err != nil {
		t.Fatalf("loading linked user: %v", err)
	}
	if linked.OidcIssuer == nil || *linked.OidcIssuer != "https://idp.example" ||
		linked.OidcSubject == nil || *linked.OidcSubject != "subject-1" {
		t.Fatalf("identity was not stored: issuer=%v subject=%v", linked.OidcIssuer, linked.OidcSubject)
	}

	// The identity wins over the email claim on later sign-ins.
	if _, err := svc.LoginOIDC(ctx, "https://idp.example", "subject-1", "renamed@example.com", "test", netip.Addr{}); err != nil {
		t.Fatalf("second single sign-on login: %v", err)
	}

	// Unknown emails are never created.
	if _, err := svc.LoginOIDC(ctx, "https://idp.example", "subject-2", "nobody@example.com", "test", netip.Addr{}); !errors.Is(err, ErrSSOUnknownUser) {
		t.Fatalf("unknown email returned %v, want ErrSSOUnknownUser", err)
	}
}

func TestOIDCStateCookieRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Now()
	st := oidcState{
		State:    "state-value",
		Nonce:    "nonce-value",
		Verifier: "verifier-value",
		Expires:  now.Add(time.Minute).Unix(),
	}
	raw, err := signOIDCState(key, st)
	if err != nil {
		t.Fatalf("signing state: %v", err)
	}
	got, err := verifyOIDCState(key, raw, now)
	if err != nil {
		t.Fatalf("verifying state: %v", err)
	}
	if got != st {
		t.Errorf("round trip = %+v, want %+v", got, st)
	}

	if _, err := verifyOIDCState([]byte("another-key"), raw, now); !errors.Is(err, ErrOIDCState) {
		t.Errorf("wrong key returned %v, want ErrOIDCState", err)
	}
	tampered := raw[:len(raw)-1] + flipBase64URL(raw[len(raw)-1])
	if _, err := verifyOIDCState(key, tampered, now); !errors.Is(err, ErrOIDCState) {
		t.Errorf("tampered cookie returned %v, want ErrOIDCState", err)
	}

	expired := st
	expired.Expires = now.Add(-time.Second).Unix()
	expiredRaw, err := signOIDCState(key, expired)
	if err != nil {
		t.Fatalf("signing expired state: %v", err)
	}
	if _, err := verifyOIDCState(key, expiredRaw, now); !errors.Is(err, ErrOIDCState) {
		t.Errorf("expired cookie returned %v, want ErrOIDCState", err)
	}
}

func flipBase64URL(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
