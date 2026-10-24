// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/atakanatamert/aicm/internal/db"
)

const (
	// OIDCStateCookieName carries state, nonce and the PKCE verifier between
	// /auth/oidc/start and /auth/oidc/callback.
	OIDCStateCookieName = "aicm_oidc_state"

	oidcStateTTL         = 5 * time.Minute
	oidcDiscoveryTimeout = 10 * time.Second
)

// Errors returned by the OIDC flow.
var (
	ErrOIDCUnavailable = errors.New("single sign-on is unavailable")
	ErrOIDCState       = errors.New("single sign-on state is invalid or expired")
	ErrOIDCUnverified  = errors.New("single sign-on email is not verified")
	ErrSSOUnknownUser  = errors.New("no active user matches this single sign-on account")
)

// OIDCConfig is the provider configuration from the environment.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Name         string
	Scopes       []string
}

// Enabled reports whether every required setting is present.
func (c OIDCConfig) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.ClientSecret != ""
}

// WithOIDC enables single sign-on. Discovery stays lazy: the hub starts even
// when the provider is unreachable.
func WithOIDC(cfg OIDCConfig) Option {
	return func(s *Service) {
		cfg.Scopes = append([]string(nil), cfg.Scopes...)
		s.oidc = &oidcClient{
			cfg:         cfg,
			redirectURL: strings.TrimRight(s.publicURL, "/") + "/auth/oidc/callback",
		}
	}
}

type oidcClient struct {
	cfg         OIDCConfig
	redirectURL string

	mu       sync.Mutex
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config

	keyOnce sync.Once
	key     []byte
	keyErr  error
}

type oidcState struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"exp"`
}

// OIDCEnabled reports whether single sign-on is configured.
func (s *Service) OIDCEnabled() bool {
	return s.oidc != nil && s.oidc.cfg.Enabled()
}

// OIDCName is the login button's label.
func (s *Service) OIDCName() string {
	if s.oidc == nil || s.oidc.cfg.Name == "" {
		return "Single sign-on"
	}
	return s.oidc.cfg.Name
}

// OIDCStart builds the provider URL and the signed cookie that carries the
// state, nonce and PKCE verifier to the callback.
func (s *Service) OIDCStart(ctx context.Context) (string, *http.Cookie, error) {
	if !s.OIDCEnabled() {
		return "", nil, ErrOIDCUnavailable
	}
	_, _, oauthCfg, err := s.oidc.discover(ctx)
	if err != nil {
		return "", nil, err
	}
	key, err := s.oidc.stateKey()
	if err != nil {
		return "", nil, err
	}
	state, err := randomURLToken(32)
	if err != nil {
		return "", nil, err
	}
	nonce, err := randomURLToken(32)
	if err != nil {
		return "", nil, err
	}
	st := oidcState{
		State:    state,
		Nonce:    nonce,
		Verifier: oauth2.GenerateVerifier(),
		Expires:  time.Now().Add(oidcStateTTL).Unix(),
	}
	raw, err := signOIDCState(key, st)
	if err != nil {
		return "", nil, err
	}
	authURL := oauthCfg.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	return authURL, s.oidcStateCookie(raw, oidcStateTTL), nil
}

// OIDCFinish validates the state cookie and the ID token, then creates a
// session for the linked user. OIDC skips TOTP: the provider runs its own
// second factor, and asking for ours again would use a factor the user may
// not have set up for this account.
func (s *Service) OIDCFinish(ctx context.Context, code, state, cookieValue, userAgent string, ip netip.Addr) (Session, error) {
	if !s.OIDCEnabled() {
		return Session{}, ErrOIDCUnavailable
	}
	if code == "" || state == "" || cookieValue == "" {
		return Session{}, ErrOIDCState
	}
	key, err := s.oidc.stateKey()
	if err != nil {
		return Session{}, err
	}
	st, err := verifyOIDCState(key, cookieValue, time.Now())
	if err != nil {
		return Session{}, err
	}
	if subtle.ConstantTimeCompare([]byte(st.State), []byte(state)) != 1 {
		return Session{}, ErrOIDCState
	}

	_, verifier, oauthCfg, err := s.oidc.discover(ctx)
	if err != nil {
		return Session{}, err
	}
	token, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return Session{}, fmt.Errorf("%w: exchanging authorization code: %w", ErrOIDCUnavailable, err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return Session{}, fmt.Errorf("%w: provider returned no ID token", ErrOIDCUnavailable)
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Session{}, fmt.Errorf("%w: verifying ID token: %w", ErrOIDCUnavailable, err)
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(st.Nonce)) != 1 {
		return Session{}, ErrOIDCState
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return Session{}, fmt.Errorf("%w: reading ID token claims: %w", ErrOIDCUnavailable, err)
	}
	if claims.Email == "" || !claims.EmailVerified {
		return Session{}, ErrOIDCUnverified
	}
	return s.LoginOIDC(ctx, s.oidc.cfg.Issuer, idToken.Subject, claims.Email, userAgent, ip)
}

// LoginOIDC finds the user by stored (issuer, subject) or by email, links
// the identity on first use and creates a session like a password login.
// OIDC never creates users.
func (s *Service) LoginOIDC(ctx context.Context, issuer, subject, email, userAgent string, ip netip.Addr) (Session, error) {
	if issuer == "" || subject == "" || strings.TrimSpace(email) == "" {
		return Session{}, ErrSSOUnknownUser
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("starting single sign-on login: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	user, err := q.GetUserByOIDCIdentity(ctx, db.GetUserByOIDCIdentityParams{
		OidcIssuer: &issuer, OidcSubject: &subject,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Session{}, fmt.Errorf("loading user by single sign-on identity: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = q.GetUserByEmail(ctx, email)
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSSOUnknownUser
		}
		if err != nil {
			return Session{}, fmt.Errorf("loading user by email: %w", err)
		}
		if user.DisabledAt != nil {
			return Session{}, ErrSSOUnknownUser
		}
		if user.OidcSubject != nil {
			// The email matches but the account is linked to another
			// identity. Never overwrite a link from an email claim.
			return Session{}, ErrSSOUnknownUser
		}
		user, err = q.LinkUserOIDC(ctx, db.LinkUserOIDCParams{
			OidcIssuer: &issuer, OidcSubject: &subject, OrgID: user.OrgID, ID: user.ID,
		})
		if errors.Is(err, pgx.ErrNoRows) || isUniqueViolation(err) {
			return Session{}, ErrSSOUnknownUser
		}
		if err != nil {
			return Session{}, fmt.Errorf("linking single sign-on identity: %w", err)
		}
	}
	if user.DisabledAt != nil {
		return Session{}, ErrSSOUnknownUser
	}

	session, err := s.createSession(ctx, q, user, userAgent, ip)
	if err != nil {
		return Session{}, err
	}
	if err := q.TouchUserLogin(ctx, db.TouchUserLoginParams{OrgID: user.OrgID, ID: user.ID}); err != nil {
		s.log.WarnContext(ctx, "recording login failed", "user_id", user.ID, "err", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("committing single sign-on login: %w", err)
	}
	return session, nil
}

// ClearedOIDCStateCookie deletes the flow cookie after a callback.
func (s *Service) ClearedOIDCStateCookie() *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows the public URL scheme; HttpOnly and SameSite are set
		Name:     OIDCStateCookieName,
		Value:    "",
		Path:     "/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.publicURL, "https://"),
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
	}
}

func (s *Service) oidcStateCookie(value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows the public URL scheme; HttpOnly and SameSite are set
		Name:     OIDCStateCookieName,
		Value:    value,
		Path:     "/auth/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(s.publicURL, "https://"),
		MaxAge:   int(ttl.Seconds()),
	}
}

// discover caches a successful provider lookup. A failure is not cached, so
// the next sign-in attempt retries a provider that was down.
func (c *oidcClient) discover(ctx context.Context) (*oidc.Provider, *oidc.IDTokenVerifier, *oauth2.Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider != nil {
		return c.provider, c.verifier, c.oauth, nil
	}
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, c.cfg.Issuer)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: discovering provider: %w", ErrOIDCUnavailable, err)
	}
	scopes := c.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	c.provider = provider
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.cfg.ClientID})
	c.oauth = &oauth2.Config{
		ClientID:     c.cfg.ClientID,
		ClientSecret: c.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  c.redirectURL,
		Scopes:       scopes,
	}
	return c.provider, c.verifier, c.oauth, nil
}

// stateKey is the HMAC key for the flow cookie. It is random per process,
// not derived from the master key: the cookie lives five minutes, a restart
// only invalidates an in-flight sign-in, and the hub runs as one process
// (sessions are in Postgres, but this cookie is not). A key derived from the
// master key would survive restarts at the cost of coupling this package to
// key rotation.
func (c *oidcClient) stateKey() ([]byte, error) {
	c.keyOnce.Do(func() {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			c.keyErr = fmt.Errorf("generating OIDC state key: %w", err)
			return
		}
		c.key = key
	})
	return c.key, c.keyErr
}

func signOIDCState(key []byte, st oidcState) (string, error) {
	payload, err := json.Marshal(st)
	if err != nil {
		return "", fmt.Errorf("encoding OIDC state: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(body))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + signature, nil
}

func verifyOIDCState(key []byte, raw string, now time.Time) (oidcState, error) {
	body, signature, ok := strings.Cut(raw, ".")
	if !ok || body == "" || signature == "" {
		return oidcState{}, ErrOIDCState
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(body))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(signature), []byte(want)) != 1 {
		return oidcState{}, ErrOIDCState
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return oidcState{}, ErrOIDCState
	}
	var st oidcState
	if err := json.Unmarshal(payload, &st); err != nil {
		return oidcState{}, ErrOIDCState
	}
	if st.State == "" || st.Nonce == "" || st.Verifier == "" || st.Expires <= now.Unix() {
		return oidcState{}, ErrOIDCState
	}
	return st, nil
}

func randomURLToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
