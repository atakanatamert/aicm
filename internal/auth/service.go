// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
)

const (
	sessionTokenPrefix = "aicm_s_"
	apiTokenPrefix     = "aicm_pat_"
	sessionTTL         = 30 * 24 * time.Hour

	// SessionCookieName is the browser session cookie.
	SessionCookieName = "aicm_session"

	loginWindow      = 10 * time.Minute
	maxLoginFailures = 5
)

// dummyPasswordHash is a valid Argon2id hash with the production parameters,
// used to keep login timing uniform when the email is unknown. It matches no
// useful password.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" //nolint:gosec // fixed dummy hash, matches no useful password

// Session is a freshly created login session.
type Session struct {
	Token     string // raw cookie value, only returned at creation
	ExpiresAt time.Time
	Principal Principal
}

// Errors returned by the auth service.
var (
	ErrInvalidLogin    = errors.New("invalid email or password")
	ErrTooManyAttempts = errors.New("too many login attempts")
	ErrSetupDone       = errors.New("setup already completed")
	ErrUnauthenticated = errors.New("not authenticated")
	ErrForbidden       = errors.New("forbidden")
)

// Service owns password checks, sessions and API token authentication.
type Service struct {
	pool         *pgxpool.Pool
	queries      *db.Queries
	log          *slog.Logger
	publicURL    string
	publicOrigin string
	limiter      loginLimiter
}

// NewService builds the auth service. publicURL is the hub's externally
// reachable base URL, used for cookie security and the CSRF origin check.
func NewService(pool *pgxpool.Pool, log *slog.Logger, publicURL string) *Service {
	return &Service{
		pool:         pool,
		queries:      db.New(pool),
		log:          log,
		publicURL:    publicURL,
		publicOrigin: originOf(publicURL),
		limiter:      loginLimiter{attempts: make(map[string][]time.Time)},
	}
}

// originOf returns scheme://host for an absolute URL, or "".
func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// SetupNeeded reports whether no user exists yet.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error) {
	n, err := s.queries.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("counting users: %w", err)
	}
	return n == 0, nil
}

// CompleteSetup creates the first admin in the default org and a session. It
// returns ErrSetupDone if any user exists.
func (s *Service) CompleteSetup(ctx context.Context, email, name, password, userAgent string, ip netip.Addr) (Session, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return Session{}, errors.New("email is required")
	}
	if err := ValidatePassword(password); err != nil {
		return Session{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Session{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("starting setup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	// Serialize concurrent first-run setup attempts.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('aicm-setup'))"); err != nil {
		return Session{}, fmt.Errorf("locking setup: %w", err)
	}
	n, err := q.CountUsers(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("counting users: %w", err)
	}
	if n > 0 {
		return Session{}, ErrSetupDone
	}
	org, err := q.GetOrgBySlug(ctx, "default")
	if err != nil {
		return Session{}, fmt.Errorf("loading default org: %w", err)
	}
	user, err := q.InsertUser(ctx, db.InsertUserParams{
		OrgID:        org.ID,
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         string(RoleAdmin),
	})
	if err != nil {
		return Session{}, fmt.Errorf("creating admin: %w", err)
	}
	session, err := s.createSession(ctx, q, user, userAgent, ip)
	if err != nil {
		return Session{}, err
	}
	if err := q.TouchUserLogin(ctx, db.TouchUserLoginParams{OrgID: user.OrgID, ID: user.ID}); err != nil {
		return Session{}, fmt.Errorf("recording login: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("committing setup: %w", err)
	}
	return session, nil
}

// Login checks the password and creates a session. Unknown emails, wrong
// passwords and disabled users all return ErrInvalidLogin.
func (s *Service) Login(ctx context.Context, email, password, userAgent string, ip netip.Addr) (Session, error) {
	email = strings.TrimSpace(email)
	key := strings.ToLower(email)
	now := time.Now()
	if s.limiter.tooMany(key, now) {
		return Session{}, ErrTooManyAttempts
	}

	user, err := s.queries.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		// Burn the same work as a real verification.
		_ = VerifyPassword(dummyPasswordHash, password)
		s.limiter.fail(key, now)
		return Session{}, ErrInvalidLogin
	}
	if err != nil {
		return Session{}, fmt.Errorf("loading user: %w", err)
	}
	if !VerifyPassword(user.PasswordHash, password) || user.DisabledAt != nil {
		s.limiter.fail(key, now)
		return Session{}, ErrInvalidLogin
	}
	s.limiter.reset(key)

	session, err := s.createSession(ctx, s.queries, user, userAgent, ip)
	if err != nil {
		return Session{}, err
	}
	if err := s.queries.TouchUserLogin(ctx, db.TouchUserLoginParams{OrgID: user.OrgID, ID: user.ID}); err != nil {
		s.log.WarnContext(ctx, "recording login failed", "user_id", user.ID, "err", err)
	}
	return session, nil
}

// Logout deletes the session behind the raw cookie value.
func (s *Service) Logout(ctx context.Context, sessionToken string) error {
	if sessionToken == "" {
		return nil
	}
	if err := s.queries.DeleteSession(ctx, hashToken(sessionToken)); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// AuthenticateSession resolves a session cookie value to a Principal.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	sum := hashToken(token)
	row, err := s.queries.GetSessionByHash(ctx, sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("loading session: %w", err)
	}
	if row.DisabledAt != nil {
		return Principal{}, ErrUnauthenticated
	}
	if err := s.queries.TouchSession(ctx, sum); err != nil {
		s.log.WarnContext(ctx, "touching session failed", "session_id", row.SessionID, "err", err)
	}
	return Principal{
		OrgID:   row.SessionOrgID,
		UserID:  row.UserID,
		Role:    Role(row.Role),
		Kind:    ActorUser,
		ActorID: row.UserID,
		Label:   row.Email,
	}, nil
}

// AuthenticateAPIToken resolves a Bearer API token to a Principal.
func (s *Service) AuthenticateAPIToken(ctx context.Context, token string) (Principal, error) {
	if token == "" || !strings.HasPrefix(token, apiTokenPrefix) {
		return Principal{}, ErrUnauthenticated
	}
	sum := hashToken(token)
	row, err := s.queries.GetAPITokenByHash(ctx, sum)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("loading API token: %w", err)
	}
	if row.DisabledAt != nil {
		return Principal{}, ErrUnauthenticated
	}
	if err := s.queries.TouchAPIToken(ctx, sum); err != nil {
		s.log.WarnContext(ctx, "touching API token failed", "token_id", row.TokenID, "err", err)
	}
	return Principal{
		OrgID:   row.TokenOrgID,
		UserID:  row.UserID,
		Role:    Role(row.Role),
		Kind:    ActorAPIToken,
		ActorID: row.TokenID,
		Label:   row.Email + " (token " + row.TokenName + ")",
	}, nil
}

// ChangePassword verifies the current password, sets the new one and deletes
// every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next, keepSessionToken string) error {
	if p.UserID == "" {
		return ErrUnauthenticated
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: p.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return fmt.Errorf("loading user: %w", err)
	}
	if user.DisabledAt != nil {
		return ErrUnauthenticated
	}
	if !VerifyPassword(user.PasswordHash, current) {
		return ErrInvalidLogin
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	if err := s.queries.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{
		OrgID:        user.OrgID,
		ID:           user.ID,
		PasswordHash: hash,
	}); err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	if err := s.queries.DeleteOtherSessions(ctx, db.DeleteOtherSessionsParams{
		OrgID:     user.OrgID,
		UserID:    user.ID,
		TokenHash: hashToken(keepSessionToken),
	}); err != nil {
		return fmt.Errorf("deleting other sessions: %w", err)
	}
	return nil
}

// Me returns the user and org for the principal.
func (s *Service) Me(ctx context.Context, p Principal) (db.User, db.Org, db.OrgSetting, error) {
	if p.UserID == "" {
		return db.User{}, db.Org{}, db.OrgSetting{}, ErrUnauthenticated
	}
	user, err := s.queries.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: p.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, db.Org{}, db.OrgSetting{}, ErrUnauthenticated
	}
	if err != nil {
		return db.User{}, db.Org{}, db.OrgSetting{}, fmt.Errorf("loading user: %w", err)
	}
	org, err := s.queries.GetOrg(ctx, p.OrgID)
	if err != nil {
		return db.User{}, db.Org{}, db.OrgSetting{}, fmt.Errorf("loading org: %w", err)
	}
	settings, err := s.queries.GetOrgSettings(ctx, p.OrgID)
	if err != nil {
		return db.User{}, db.Org{}, db.OrgSetting{}, fmt.Errorf("loading org settings: %w", err)
	}
	return user, org, settings, nil
}

// CreateAdmin creates an admin in the default org even when users already
// exist. It is used by the create-admin command.
func (s *Service) CreateAdmin(ctx context.Context, email, name, password string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return errors.New("email is required")
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	org, err := s.queries.GetOrgBySlug(ctx, "default")
	if err != nil {
		return fmt.Errorf("loading default org: %w", err)
	}
	if _, err := s.queries.InsertUser(ctx, db.InsertUserParams{
		OrgID:        org.ID,
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         string(RoleAdmin),
	}); err != nil {
		return fmt.Errorf("creating admin: %w", err)
	}
	return nil
}

// SessionCookie returns the browser cookie holding a raw session token.
func (s *Service) SessionCookie(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows the public URL scheme; HttpOnly and SameSite are set
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.publicURL, "https://"),
		Expires:  expires,
	}
}

// ClearedSessionCookie returns a cookie that deletes aicm_session.
func (s *Service) ClearedSessionCookie() *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows the public URL scheme; HttpOnly and SameSite are set
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.publicURL, "https://"),
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
	}
}

// createSession inserts a session and returns its raw token.
func (s *Service) createSession(ctx context.Context, q *db.Queries, user db.User, userAgent string, ip netip.Addr) (Session, error) {
	token, sum, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	var ipPtr *netip.Addr
	if ip.IsValid() {
		ipPtr = &ip
	}
	row, err := q.InsertSession(ctx, db.InsertSessionParams{
		OrgID:     user.OrgID,
		UserID:    user.ID,
		TokenHash: sum,
		UserAgent: userAgent,
		Ip:        ipPtr,
		ExpiresAt: time.Now().UTC().Add(sessionTTL),
	})
	if err != nil {
		return Session{}, fmt.Errorf("creating session: %w", err)
	}
	return Session{
		Token:     token,
		ExpiresAt: row.ExpiresAt,
		Principal: Principal{
			OrgID:   user.OrgID,
			UserID:  user.ID,
			Role:    Role(user.Role),
			Kind:    ActorUser,
			ActorID: user.ID,
			Label:   user.Email,
		},
	}, nil
}

func newSessionToken() (string, []byte, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, fmt.Errorf("generating session token: %w", err)
	}
	token := sessionTokenPrefix + base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// loginLimiter tracks failed logins per email in memory.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

// tooMany reports whether key already has the maximum failures in the window.
func (l *loginLimiter) tooMany(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	cutoff := now.Add(-loginWindow)
	attempts := l.attempts[key]
	i := 0
	for i < len(attempts) && attempts[i].Before(cutoff) {
		i++
	}
	attempts = attempts[i:]
	if len(attempts) == 0 {
		delete(l.attempts, key)
		return false
	}
	l.attempts[key] = attempts
	return len(attempts) >= maxLoginFailures
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts[key] = append(l.attempts[key], now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// sweep drops expired entries once the map grows past a small bound, so
// arbitrary email guesses cannot grow it without limit.
func (l *loginLimiter) sweep(now time.Time) {
	if len(l.attempts) <= 1024 {
		return
	}
	cutoff := now.Add(-loginWindow)
	for key, attempts := range l.attempts {
		kept := attempts[:0]
		for _, at := range attempts {
			if !at.Before(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = kept
		}
	}
}
