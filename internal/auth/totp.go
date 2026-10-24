// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP is defined with HMAC-SHA-1
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// Two-factor sign-in (RFC 6238). Codes are SHA-1, six digits, 30-second
// steps, and the clock may be one step off in either direction.
const (
	totpSecretBytes  = 20
	totpDigits       = 6
	totpStepSeconds  = 30
	totpSkewSteps    = 1
	recoveryCount    = 10
	recoveryLength   = 10
	recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

	challengeTTL         = 5 * time.Minute
	maxChallengeAttempts = 5
)

// Errors returned by the two-factor methods.
var (
	ErrInvalidCode        = errors.New("invalid code")
	ErrChallengeExpired   = errors.New("login challenge expired")
	ErrTotpAlreadyEnabled = errors.New("two-factor sign-in is already enabled")
	ErrTotpNotSetup       = errors.New("two-factor setup has not been started")
)

// TOTPStatus describes a user's second factor without exposing the secret.
type TOTPStatus struct {
	Enabled           bool
	EnabledAt         *time.Time
	RecoveryCodesLeft int
}

// GenerateTOTPSecret returns 20 random bytes as unpadded base32.
func GenerateTOTPSecret() (string, error) {
	var raw [totpSecretBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating TOTP secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]), nil
}

// OTPAuthURL builds the otpauth:// URI an authenticator app scans.
func OTPAuthURL(email, secret string) string {
	return "otpauth://totp/" + url.PathEscape("aicm:"+email) +
		"?secret=" + url.QueryEscape(secret) + "&issuer=aicm"
}

// TOTPCode returns the six-digit code for secret at time at.
func TOTPCode(secret string, at time.Time) (string, error) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return "", err
	}
	return formatTOTP(totpValue(key, at.Unix()/totpStepSeconds)), nil
}

// ValidateTOTP accepts code when it matches the step for at or one of its
// neighbours. It returns the matched step so callers can refuse a replay of
// a code already accepted in that step.
func ValidateTOTP(secret, code string, at time.Time) (int64, bool) {
	key, err := decodeTOTPSecret(secret)
	if err != nil {
		return 0, false
	}
	code = normalizeCode(code)
	if len(code) != totpDigits || !allDigits(code) {
		return 0, false
	}
	current := at.Unix() / totpStepSeconds
	// Current step first; neighbours cover clock skew.
	for _, step := range []int64{current, current - 1, current + 1} {
		want := formatTOTP(totpValue(key, step))
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func decodeTOTPSecret(secret string) ([]byte, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	normalized = strings.TrimRight(normalized, "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return nil, fmt.Errorf("decoding TOTP secret: %w", err)
	}
	return key, nil
}

func totpValue(key []byte, step int64) uint32 {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) //nolint:gosec // Unix time steps are non-negative
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := uint32(sum[offset]&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return value % 1_000_000
}

func formatTOTP(value uint32) string {
	return fmt.Sprintf("%0*d", totpDigits, value)
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeCode strips the spaces and dashes people add when typing a code.
func normalizeCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r == ' ' || r == '\t' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// GenerateRecoveryCodes returns ten single-use codes from an alphabet without
// look-alike characters (no 0/O, 1/I).
func GenerateRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCount)
	seen := make(map[string]struct{}, recoveryCount)
	for len(codes) < recoveryCount {
		var raw [recoveryLength]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, fmt.Errorf("generating recovery codes: %w", err)
		}
		code := make([]byte, recoveryLength)
		for i, b := range raw {
			code[i] = recoveryAlphabet[int(b)%len(recoveryAlphabet)]
		}
		if _, dup := seen[string(code)]; dup {
			continue
		}
		seen[string(code)] = struct{}{}
		codes = append(codes, string(code))
	}
	return codes, nil
}

// HashRecoveryCode returns the SHA-256 stored for a recovery code. Only the
// hash is ever written; the code itself is shown once.
func HashRecoveryCode(code string) []byte {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return sum[:]
}

// stepTracker remembers the last TOTP step accepted per user, in memory.
//
// A challenge is already single use, so this only closes the window in which
// an attacker who observes a code could redeem it through a second login
// challenge in the same 30-second step. Memory is enough: the hub runs as one
// process, a restart forgets at most the last step, and an accepted code is
// only valid until its own step ends anyway.
type stepTracker struct {
	mu   sync.Mutex
	last map[string]int64
}

// accept records step for user and reports false when that step (or an older
// one) was already accepted.
func (t *stepTracker) accept(userID string, step, nowStep int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last == nil {
		t.last = make(map[string]int64)
	}
	if previous, ok := t.last[userID]; ok && step <= previous {
		return false
	}
	t.last[userID] = step
	if len(t.last) > 4096 {
		for id, accepted := range t.last {
			if accepted < nowStep-2 {
				delete(t.last, id)
			}
		}
	}
	return true
}

// GetTOTPStatus returns whether the user has two-factor on and how many
// recovery codes are unused.
func (s *Service) GetTOTPStatus(ctx context.Context, p Principal) (TOTPStatus, error) {
	user, err := s.totpUser(ctx, p)
	if err != nil {
		return TOTPStatus{}, err
	}
	left, err := s.queries.CountUnusedRecoveryCodes(ctx, db.CountUnusedRecoveryCodesParams{
		OrgID: p.OrgID, UserID: p.UserID,
	})
	if err != nil {
		return TOTPStatus{}, fmt.Errorf("counting recovery codes: %w", err)
	}
	return TOTPStatus{
		Enabled:           user.TotpEnabledAt != nil,
		EnabledAt:         user.TotpEnabledAt,
		RecoveryCodesLeft: int(left),
	}, nil
}

// SetupTOTP creates a new pending secret and returns it with its otpauth
// URL. It replaces an earlier pending secret. Enabling is a separate step so
// a mistake in the app does not lock the user out.
func (s *Service) SetupTOTP(ctx context.Context, p Principal) (string, string, error) {
	if s.secrets == nil {
		return "", "", errors.New("two-factor requires the secret store")
	}
	user, err := s.totpUser(ctx, p)
	if err != nil {
		return "", "", err
	}
	if user.TotpEnabledAt != nil {
		return "", "", ErrTotpAlreadyEnabled
	}

	secret, err := GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	info, err := s.secrets.Put(ctx, p.OrgID, secret)
	if err != nil {
		return "", "", fmt.Errorf("storing TOTP secret: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		_ = s.deleteSecret(ctx, p.OrgID, info.ID)
		return "", "", fmt.Errorf("starting two-factor setup: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	n, err := q.SetTotpPendingSecret(ctx, db.SetTotpPendingSecretParams{
		TotpSecretID: &info.ID, OrgID: p.OrgID, ID: p.UserID,
	})
	if err != nil {
		_ = s.deleteSecret(ctx, p.OrgID, info.ID)
		return "", "", fmt.Errorf("storing pending TOTP secret: %w", err)
	}
	if n == 0 {
		_ = s.deleteSecret(ctx, p.OrgID, info.ID)
		return "", "", ErrTotpAlreadyEnabled
	}
	if err := s.auditChange(ctx, q, p, "user.totp_setup", "user", p.UserID,
		map[string]any{"pending": user.TotpPendingSecretID != nil}, map[string]any{"pending": true}); err != nil {
		_ = s.deleteSecret(ctx, p.OrgID, info.ID)
		return "", "", err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = s.deleteSecret(ctx, p.OrgID, info.ID)
		return "", "", fmt.Errorf("committing two-factor setup: %w", err)
	}

	// The old pending secret is no longer referenced.
	if user.TotpPendingSecretID != nil && *user.TotpPendingSecretID != info.ID {
		_ = s.deleteSecret(ctx, p.OrgID, *user.TotpPendingSecretID)
	}
	return secret, OTPAuthURL(user.Email, secret), nil
}

// EnableTOTP checks code against the pending secret, turns two-factor on and
// returns ten fresh recovery codes. The codes are shown only in this result.
func (s *Service) EnableTOTP(ctx context.Context, p Principal, code string) ([]string, error) {
	if s.secrets == nil {
		return nil, errors.New("two-factor requires the secret store")
	}
	user, err := s.totpUser(ctx, p)
	if err != nil {
		return nil, err
	}
	if user.TotpEnabledAt != nil {
		return nil, ErrTotpAlreadyEnabled
	}
	if user.TotpPendingSecretID == nil {
		return nil, ErrTotpNotSetup
	}
	secret, err := s.secrets.Reveal(ctx, p.OrgID, *user.TotpPendingSecretID)
	if errors.Is(err, secrets.ErrNotFound) {
		return nil, ErrTotpNotSetup
	}
	if err != nil {
		return nil, fmt.Errorf("loading pending TOTP secret: %w", err)
	}
	if _, ok := ValidateTOTP(secret, code, time.Now()); !ok {
		return nil, ErrInvalidCode
	}

	codes, err := GenerateRecoveryCodes()
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting two-factor enable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	n, err := q.EnableTotp(ctx, db.EnableTotpParams{
		OrgID: p.OrgID, ID: p.UserID, PendingSecretID: user.TotpPendingSecretID,
	})
	if err != nil {
		return nil, fmt.Errorf("enabling two-factor: %w", err)
	}
	if n == 0 {
		current, err := q.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: p.UserID})
		if err == nil && current.TotpEnabledAt != nil {
			return nil, ErrTotpAlreadyEnabled
		}
		return nil, ErrTotpNotSetup
	}
	if err := q.DeleteRecoveryCodes(ctx, db.DeleteRecoveryCodesParams{OrgID: p.OrgID, UserID: p.UserID}); err != nil {
		return nil, fmt.Errorf("replacing recovery codes: %w", err)
	}
	for _, recovery := range codes {
		if err := q.InsertRecoveryCode(ctx, db.InsertRecoveryCodeParams{
			OrgID: p.OrgID, UserID: p.UserID, CodeHash: HashRecoveryCode(recovery),
		}); err != nil {
			return nil, fmt.Errorf("storing recovery code: %w", err)
		}
	}
	if err := s.auditChange(ctx, q, p, "user.totp_enable", "user", p.UserID,
		map[string]any{"enabled": false},
		map[string]any{"enabled": true, "recovery_codes": len(codes)}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing two-factor enable: %w", err)
	}
	return codes, nil
}

// DisableTOTP verifies the password and a current code (or a recovery code),
// then removes the secret, the enabled time and every recovery code.
func (s *Service) DisableTOTP(ctx context.Context, p Principal, password, code string) error {
	user, err := s.totpUser(ctx, p)
	if err != nil {
		return err
	}
	if !VerifyPassword(user.PasswordHash, password) {
		return ErrInvalidLogin
	}
	if user.TotpEnabledAt == nil {
		return nil
	}

	totpOK := false
	if user.TotpSecretID != nil && s.secrets != nil {
		secret, err := s.secrets.Reveal(ctx, p.OrgID, *user.TotpSecretID)
		switch {
		case err == nil:
			_, totpOK = ValidateTOTP(secret, code, time.Now())
		case errors.Is(err, secrets.ErrNotFound):
			// Fall through to the recovery code.
		default:
			return fmt.Errorf("loading TOTP secret: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting two-factor disable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	if !totpOK {
		n, err := q.ConsumeRecoveryCode(ctx, db.ConsumeRecoveryCodeParams{
			OrgID: p.OrgID, UserID: p.UserID, CodeHash: HashRecoveryCode(code),
		})
		if err != nil {
			return fmt.Errorf("checking recovery code: %w", err)
		}
		if n == 0 {
			return ErrInvalidCode
		}
	}
	if _, err := q.DisableTotp(ctx, db.DisableTotpParams{OrgID: p.OrgID, ID: p.UserID}); err != nil {
		return fmt.Errorf("disabling two-factor: %w", err)
	}
	if err := q.DeleteRecoveryCodes(ctx, db.DeleteRecoveryCodesParams{OrgID: p.OrgID, UserID: p.UserID}); err != nil {
		return fmt.Errorf("deleting recovery codes: %w", err)
	}
	if err := s.auditChange(ctx, q, p, "user.totp_disable", "user", p.UserID,
		map[string]any{"enabled": true}, map[string]any{"enabled": false}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing two-factor disable: %w", err)
	}
	s.deleteTotpSecrets(ctx, p.OrgID, user)
	return nil
}

// ResetUserTOTP turns off another user's two-factor when they lost their
// phone. Only an admin may do it, and it needs no code from that user.
func (s *Service) ResetUserTOTP(ctx context.Context, p Principal, userID string) error {
	if p.UserID == "" {
		return ErrUnauthenticated
	}
	if !p.IsAdmin() {
		return ErrForbidden
	}
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: userID})
	if isMissing(err) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("loading user: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting two-factor reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	if _, err := q.DisableTotp(ctx, db.DisableTotpParams{OrgID: p.OrgID, ID: userID}); err != nil {
		return fmt.Errorf("resetting two-factor: %w", err)
	}
	if err := q.DeleteRecoveryCodes(ctx, db.DeleteRecoveryCodesParams{OrgID: p.OrgID, UserID: userID}); err != nil {
		return fmt.Errorf("deleting recovery codes: %w", err)
	}
	if err := s.auditChange(ctx, q, p, "user.totp_reset", "user", userID,
		map[string]any{"enabled": user.TotpEnabledAt != nil},
		map[string]any{"enabled": false}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing two-factor reset: %w", err)
	}
	s.deleteTotpSecrets(ctx, p.OrgID, user)
	return nil
}

// LoginTOTP finishes a login that returned a challenge. code is a six-digit
// app code or an unused recovery code.
func (s *Service) LoginTOTP(ctx context.Context, challenge, code, userAgent string, ip netip.Addr) (Session, error) {
	if challenge == "" {
		return Session{}, ErrChallengeExpired
	}
	now := time.Now()
	sum := hashToken(challenge)
	row, err := s.queries.GetLoginChallenge(ctx, sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrChallengeExpired
	}
	if err != nil {
		return Session{}, fmt.Errorf("loading login challenge: %w", err)
	}
	if row.DisabledAt != nil {
		return Session{}, ErrInvalidLogin
	}
	if challengeExpired(row.ExpiresAt, now) {
		_, _ = s.queries.DeleteLoginChallenge(ctx, db.DeleteLoginChallengeParams{OrgID: row.OrgID, ID: row.ID})
		return Session{}, ErrChallengeExpired
	}
	key := strings.ToLower(row.Email)
	if s.limiter.tooMany(key, now) || attemptsExhausted(row.Attempts) {
		_, _ = s.queries.DeleteLoginChallenge(ctx, db.DeleteLoginChallengeParams{OrgID: row.OrgID, ID: row.ID})
		return Session{}, ErrTooManyAttempts
	}

	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{OrgID: row.OrgID, ID: row.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrChallengeExpired
	}
	if err != nil {
		return Session{}, fmt.Errorf("loading user: %w", err)
	}
	if user.DisabledAt != nil {
		return Session{}, ErrInvalidLogin
	}

	code = normalizeCode(code)
	totpOK := false
	if user.TotpSecretID != nil && s.secrets != nil {
		secret, err := s.secrets.Reveal(ctx, row.OrgID, *user.TotpSecretID)
		switch {
		case err == nil:
			var step int64
			if step, totpOK = ValidateTOTP(secret, code, now); totpOK {
				totpOK = s.steps.accept(user.ID, step, now.Unix()/totpStepSeconds)
			}
		case errors.Is(err, secrets.ErrNotFound):
		default:
			return Session{}, fmt.Errorf("loading TOTP secret: %w", err)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("starting two-factor login: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	// Deleting the challenge inside the transaction makes it single use even
	// when two requests arrive together.
	n, err := q.DeleteLoginChallenge(ctx, db.DeleteLoginChallengeParams{OrgID: row.OrgID, ID: row.ID})
	if err != nil {
		return Session{}, fmt.Errorf("consuming login challenge: %w", err)
	}
	if n == 0 {
		return Session{}, ErrChallengeExpired
	}
	if !totpOK {
		n, err := q.ConsumeRecoveryCode(ctx, db.ConsumeRecoveryCodeParams{
			OrgID: row.OrgID, UserID: row.UserID, CodeHash: HashRecoveryCode(code),
		})
		if err != nil {
			return Session{}, fmt.Errorf("checking recovery code: %w", err)
		}
		if n == 0 {
			// Put the challenge back: this attempt was wrong but the user
			// may still try again. Release the row lock before counting it.
			_ = tx.Rollback(ctx)
			return Session{}, s.failLoginChallenge(ctx, row, key, now)
		}
	}

	session, err := s.createSession(ctx, q, user, userAgent, ip)
	if err != nil {
		return Session{}, err
	}
	if err := q.TouchUserLogin(ctx, db.TouchUserLoginParams{OrgID: user.OrgID, ID: user.ID}); err != nil {
		s.log.WarnContext(ctx, "recording login failed", "user_id", user.ID, "err", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("committing two-factor login: %w", err)
	}
	s.limiter.reset(key)
	return session, nil
}

// createLoginChallenge stores the hash of a fresh 32-byte token and returns
// the raw token for the browser.
func (s *Service) createLoginChallenge(ctx context.Context, user db.User) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generating login challenge: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := s.queries.DeleteExpiredLoginChallenges(ctx, db.DeleteExpiredLoginChallengesParams{
		OrgID: user.OrgID, UserID: user.ID,
	}); err != nil {
		return "", fmt.Errorf("cleaning login challenges: %w", err)
	}
	if _, err := s.queries.InsertLoginChallenge(ctx, db.InsertLoginChallengeParams{
		OrgID:     user.OrgID,
		UserID:    user.ID,
		TokenHash: hashToken(token),
		ExpiresAt: time.Now().UTC().Add(challengeTTL),
	}); err != nil {
		return "", fmt.Errorf("creating login challenge: %w", err)
	}
	return token, nil
}

// failLoginChallenge counts one wrong code, ends the challenge after the
// fifth and feeds the same per-email limiter as password failures.
func (s *Service) failLoginChallenge(ctx context.Context, row db.GetLoginChallengeRow, key string, now time.Time) error {
	attempts, err := s.queries.IncrementLoginChallengeAttempts(ctx, db.IncrementLoginChallengeAttemptsParams{
		OrgID: row.OrgID, ID: row.ID,
	})
	if err != nil {
		return fmt.Errorf("counting failed code: %w", err)
	}
	s.limiter.fail(key, now)
	if attemptsExhausted(attempts) || s.limiter.tooMany(key, now) {
		_, _ = s.queries.DeleteLoginChallenge(ctx, db.DeleteLoginChallengeParams{OrgID: row.OrgID, ID: row.ID})
		return ErrTooManyAttempts
	}
	return ErrInvalidCode
}

func (s *Service) totpUser(ctx context.Context, p Principal) (db.User, error) {
	if p.UserID == "" {
		return db.User{}, ErrUnauthenticated
	}
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: p.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrUnauthenticated
	}
	if err != nil {
		return db.User{}, fmt.Errorf("loading user: %w", err)
	}
	return user, nil
}

// deleteTotpSecrets removes the encrypted secrets a disabled user no longer
// references. Failure leaves encrypted, unused rows behind; the user's
// columns are already cleared.
func (s *Service) deleteTotpSecrets(ctx context.Context, orgID string, user db.User) {
	for _, id := range []*string{user.TotpSecretID, user.TotpPendingSecretID} {
		if id != nil {
			_ = s.deleteSecret(ctx, orgID, *id)
		}
	}
}

func (s *Service) deleteSecret(ctx context.Context, orgID, secretID string) error {
	if s.secrets == nil {
		return nil
	}
	if err := s.secrets.Delete(ctx, orgID, secretID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		s.log.WarnContext(ctx, "deleting two-factor secret failed", "err", err)
		return err
	}
	return nil
}

func challengeExpired(expiresAt, now time.Time) bool {
	return !now.Before(expiresAt)
}

func attemptsExhausted(attempts int32) bool {
	return attempts >= maxChallengeAttempts
}
