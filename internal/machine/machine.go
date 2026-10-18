// SPDX-License-Identifier: AGPL-3.0-or-later

// Package machine enrols agents, stores their per-machine credentials and
// manages the org's Ed25519 hub signing key.
package machine

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/secrets"
	"github.com/atakanatamert/aicm/internal/version"
)

// repoPath is the project's source repository. A project rename updates this
// constant and version.Name only.
const repoPath = "atakanatamert/aicm"

// scanIntervalSeconds is how often the hub asks an agent to scan harnesses.
const scanIntervalSeconds = 60

// ErrNotFound means the org has no such machine or enrollment token.
var ErrNotFound = errors.New("not found")

// Service owns enrollment tokens, machines and the org's hub key.
type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	store     secrets.Store
	publicURL string
	log       *slog.Logger
}

var _ agentlink.Hub = (*Service)(nil)

// NewService builds the machine service. publicURL is the hub's externally
// reachable base URL, without a trailing slash.
func NewService(pool *pgxpool.Pool, store secrets.Store, publicURL string, log *slog.Logger) *Service {
	return &Service{
		pool:      pool,
		q:         db.New(pool),
		store:     store,
		publicURL: strings.TrimRight(publicURL, "/"),
		log:       log,
	}
}

// CreatedToken is a new enrollment token and the one-line install commands.
type CreatedToken struct {
	Token                       db.EnrollToken
	Raw                         string // shown once
	HubURL, OrgID, HubPublicKey string // base64 std of the public key
	Commands                    Commands
}

// Commands are the one-line installs for the supported platforms.
type Commands struct{ Sh, Brew, PowerShell, Docker string }

// HubPublicKey returns the org's Ed25519 public key, creating the key pair on
// first use. The private seed is stored through the secret store only.
func (s *Service) HubPublicKey(ctx context.Context, orgID string) (ed25519.PublicKey, error) {
	row, err := s.q.GetHubKey(ctx, orgID)
	if err == nil {
		return ed25519.PublicKey(row.PublicKey), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("loading hub key: %w", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating hub key: %w", err)
	}
	seed := priv.Seed()
	encoded := base64.StdEncoding.EncodeToString(seed)
	clear(seed)
	info, err := s.store.Put(ctx, orgID, encoded)
	if err != nil {
		return nil, fmt.Errorf("storing hub signing key: %w", err)
	}

	row, err = s.q.InsertHubKey(ctx, db.InsertHubKeyParams{
		OrgID:     orgID,
		PublicKey: pub,
		SecretID:  info.ID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Another caller created the key first; use theirs and drop ours.
		s.discardSecret(ctx, orgID, info.ID)
		row, err = s.q.GetHubKey(ctx, orgID)
		if err != nil {
			return nil, fmt.Errorf("loading hub key after conflict: %w", err)
		}
		return ed25519.PublicKey(row.PublicKey), nil
	case err != nil:
		s.discardSecret(ctx, orgID, info.ID)
		return nil, fmt.Errorf("recording hub key: %w", err)
	}
	return ed25519.PublicKey(row.PublicKey), nil
}

// discardSecret removes a secret made redundant by a race. Failure leaves an
// unused secret behind, never a second key.
func (s *Service) discardSecret(ctx context.Context, orgID, secretID string) {
	if err := s.store.Delete(ctx, orgID, secretID); err != nil {
		s.log.WarnContext(ctx, "could not delete losing hub key secret", "secret_id", secretID, "err", err)
	}
}

// SigningKey returns the org's private hub key. Unknown orgs and orgs
// without a key return an error.
func (s *Service) SigningKey(ctx context.Context, orgID string) (ed25519.PrivateKey, error) {
	row, err := s.q.GetHubKey(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("loading hub key: %w", err)
	}
	encoded, err := s.store.Reveal(ctx, orgID, row.SecretID)
	if err != nil {
		return nil, fmt.Errorf("revealing hub key: %w", err)
	}
	seed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding hub key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		clear(seed)
		return nil, fmt.Errorf("hub key seed has the wrong length: %d", len(seed))
	}
	key := ed25519.NewKeyFromSeed(seed)
	clear(seed)
	return key, nil
}

// Authenticate implements agentlink.Hub for both credential kinds.
func (s *Service) Authenticate(ctx context.Context, hello agentlink.Hello, a agentlink.Auth) (agentlink.Welcome, error) {
	switch {
	case a.EnrollToken != "":
		return s.authenticateEnrollToken(ctx, hello, a.EnrollToken)
	case a.MachineToken != "":
		return s.authenticateMachineToken(ctx, hello, a.MachineToken)
	default:
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "invalid credential"}
	}
}

func (s *Service) authenticateEnrollToken(ctx context.Context, hello agentlink.Hello, raw string) (agentlink.Welcome, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentlink.Welcome{}, fmt.Errorf("starting enrollment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	token, err := q.GetUsableEnrollToken(ctx, db.GetUsableEnrollTokenParams{
		OrgID:     hello.OrgID,
		TokenHash: agentlink.HashToken(raw),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "invalid enroll token"}
	}
	if err != nil {
		return agentlink.Welcome{}, fmt.Errorf("loading enroll token: %w", err)
	}
	if _, err := q.TouchEnrollToken(ctx, db.TouchEnrollTokenParams{OrgID: hello.OrgID, ID: token.ID}); err != nil {
		return agentlink.Welcome{}, fmt.Errorf("recording enroll token use: %w", err)
	}

	machineToken := agentlink.NewToken(agentlink.MachineTokenPrefix)
	m, created, err := s.enrollMachine(ctx, q, hello, token.ID, machineToken)
	if err != nil {
		return agentlink.Welcome{}, err
	}
	if created {
		if err := audit.Write(ctx, q, auth.Principal{
			OrgID: hello.OrgID,
			Kind:  auth.ActorSystem,
			Label: hello.Hostname,
		}.Actor(), audit.Entry{
			Action:     "machine.enroll",
			TargetKind: "machine",
			TargetID:   m.ID,
			After: map[string]any{
				"hostname":      m.Hostname,
				"os":            m.Os,
				"arch":          m.Arch,
				"agent_version": m.AgentVersion,
			},
		}); err != nil {
			return agentlink.Welcome{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return agentlink.Welcome{}, fmt.Errorf("committing enrollment: %w", err)
	}
	return agentlink.Welcome{
		MachineID:    m.ID,
		MachineToken: machineToken,
		Config:       agentConfig(),
	}, nil
}

// enrollMachine rotates the token of an existing machine with the same
// fingerprint, or creates the machine. created reports which happened.
func (s *Service) enrollMachine(ctx context.Context, q *db.Queries, hello agentlink.Hello, enrollTokenID, machineToken string) (db.Machine, bool, error) {
	existing, err := q.GetMachineByFingerprint(ctx, db.GetMachineByFingerprintParams{
		OrgID:       hello.OrgID,
		Fingerprint: hello.Fingerprint,
	})
	switch {
	case err == nil:
		if existing.RevokedAt != nil {
			return db.Machine{}, false, &agentlink.Error{Code: agentlink.CodeRevoked, Message: "machine revoked"}
		}
		m, err := q.UpdateMachineToken(ctx, db.UpdateMachineTokenParams{
			TokenHash:     agentlink.HashToken(machineToken),
			EnrollTokenID: &enrollTokenID,
			OrgID:         hello.OrgID,
			ID:            existing.ID,
		})
		if err != nil {
			return db.Machine{}, false, fmt.Errorf("rotating machine token: %w", err)
		}
		return m, false, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return db.Machine{}, false, fmt.Errorf("looking up machine: %w", err)
	}

	m, err := q.InsertMachine(ctx, db.InsertMachineParams{
		OrgID:         hello.OrgID,
		Name:          hello.Hostname,
		Hostname:      hello.Hostname,
		Fingerprint:   hello.Fingerprint,
		Os:            hello.OS,
		Arch:          hello.Arch,
		AgentVersion:  hello.AgentVersion,
		TokenHash:     agentlink.HashToken(machineToken),
		EnrollTokenID: &enrollTokenID,
	})
	if err != nil {
		return db.Machine{}, false, fmt.Errorf("creating machine: %w", err)
	}
	return m, true, nil
}

func (s *Service) authenticateMachineToken(ctx context.Context, hello agentlink.Hello, raw string) (agentlink.Welcome, error) {
	m, err := s.q.GetMachineByTokenHash(ctx, db.GetMachineByTokenHashParams{
		OrgID:     hello.OrgID,
		TokenHash: agentlink.HashToken(raw),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "unknown machine token"}
	}
	if err != nil {
		return agentlink.Welcome{}, fmt.Errorf("loading machine: %w", err)
	}
	if m.RevokedAt != nil {
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeRevoked, Message: "machine revoked"}
	}
	if m.Fingerprint != hello.Fingerprint {
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeFingerprint, Message: "fingerprint changed"}
	}
	m, err = s.q.TouchMachine(ctx, db.TouchMachineParams{
		AgentVersion: hello.AgentVersion,
		Os:           hello.OS,
		Arch:         hello.Arch,
		Hostname:     hello.Hostname,
		OrgID:        hello.OrgID,
		ID:           m.ID,
	})
	if err != nil {
		return agentlink.Welcome{}, fmt.Errorf("recording machine contact: %w", err)
	}
	return agentlink.Welcome{MachineID: m.ID, Config: agentConfig()}, nil
}

func agentConfig() agentlink.AgentConfig {
	return agentlink.AgentConfig{Accounts: []agentlink.AccountConfig{}, ScanIntervalSeconds: scanIntervalSeconds}
}

// CreateEnrollToken creates a universal token and its install commands.
func (s *Service) CreateEnrollToken(ctx context.Context, p auth.Principal, permanent bool, ttl time.Duration) (CreatedToken, error) {
	pub, err := s.HubPublicKey(ctx, p.OrgID)
	if err != nil {
		return CreatedToken{}, err
	}
	raw := agentlink.NewToken(agentlink.EnrollTokenPrefix)
	var expiresAt *time.Time
	if !permanent {
		t := time.Now().UTC().Add(ttl)
		expiresAt = &t
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return CreatedToken{}, fmt.Errorf("starting enroll token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	row, err := q.InsertEnrollToken(ctx, db.InsertEnrollTokenParams{
		OrgID:     p.OrgID,
		TokenHash: agentlink.HashToken(raw),
		CreatedBy: p.UserID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return CreatedToken{}, fmt.Errorf("creating enroll token: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "enroll_token.create",
		TargetKind: "enroll_token",
		TargetID:   row.ID,
		After:      map[string]any{"id": row.ID, "expires_at": row.ExpiresAt},
	}); err != nil {
		return CreatedToken{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CreatedToken{}, fmt.Errorf("committing enroll token: %w", err)
	}

	key := base64.StdEncoding.EncodeToString(pub)
	return CreatedToken{
		Token:        row,
		Raw:          raw,
		HubURL:       s.publicURL,
		OrgID:        p.OrgID,
		HubPublicKey: key,
		Commands:     s.commands(p.OrgID, key, raw),
	}, nil
}

// commands renders the install one-liners from the public values.
func (s *Service) commands(orgID, publicKey, token string) Commands {
	hub := s.publicURL
	owner, _, _ := strings.Cut(repoPath, "/")
	binary := version.AgentBinary
	sh := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	ps := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	enroll := "--hub " + sh(hub) + " --org " + sh(orgID) + " --key " + sh(publicKey) + " --token " + sh(token)
	return Commands{
		Sh: fmt.Sprintf("curl -fsSL %s/install.sh | sh -s -- %s", hub, enroll),
		Brew: fmt.Sprintf("brew install %s/tap/%s && %s enroll %s && brew services start %s",
			owner, binary, binary, enroll, binary),
		PowerShell: fmt.Sprintf("& ([scriptblock]::Create((irm %s))) -Hub %s -Org %s -Key %s -Token %s",
			ps(hub+"/install.ps1"), ps(hub), ps(orgID), ps(publicKey), ps(token)),
		Docker: fmt.Sprintf(`docker run -d --name %s --restart unless-stopped -v "$HOME:/host-home:ro" -e AICM_AGENT_HOME=/host-home ghcr.io/%s/%s:latest run %s`,
			binary, owner, binary, enroll),
	}
}

// ListEnrollTokens returns every enrollment token in the org, hashes only.
func (s *Service) ListEnrollTokens(ctx context.Context, p auth.Principal) ([]db.EnrollToken, error) {
	rows, err := s.q.ListEnrollTokens(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing enroll tokens: %w", err)
	}
	return rows, nil
}

// RevokeEnrollToken revokes one enrollment token. Machines already enrolled
// keep their machine tokens.
func (s *Service) RevokeEnrollToken(ctx context.Context, p auth.Principal, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting revoke transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	row, err := q.RevokeEnrollToken(ctx, db.RevokeEnrollTokenParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("revoking enroll token: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "enroll_token.revoke",
		TargetKind: "enroll_token",
		TargetID:   row.ID,
		After:      map[string]any{"revoked_at": row.RevokedAt},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing revoke: %w", err)
	}
	return nil
}

// ListMachines returns every machine in the org.
func (s *Service) ListMachines(ctx context.Context, p auth.Principal) ([]db.Machine, error) {
	rows, err := s.q.ListMachines(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing machines: %w", err)
	}
	return rows, nil
}

// RenameMachine changes a machine's display name.
func (s *Service) RenameMachine(ctx context.Context, p auth.Principal, id, name string) (db.Machine, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return db.Machine{}, errors.New("name is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Machine{}, fmt.Errorf("starting rename transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetMachine(ctx, db.GetMachineParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Machine{}, ErrNotFound
	}
	if err != nil {
		return db.Machine{}, fmt.Errorf("loading machine: %w", err)
	}
	after, err := q.RenameMachine(ctx, db.RenameMachineParams{OrgID: p.OrgID, ID: id, Name: name})
	if err != nil {
		return db.Machine{}, fmt.Errorf("renaming machine: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "machine.rename",
		TargetKind: "machine",
		TargetID:   after.ID,
		Before:     map[string]any{"name": before.Name},
		After:      map[string]any{"name": after.Name},
	}); err != nil {
		return db.Machine{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Machine{}, fmt.Errorf("committing rename: %w", err)
	}
	return after, nil
}

// RevokeMachine revokes a machine's credential. The agent can no longer
// reconnect.
func (s *Service) RevokeMachine(ctx context.Context, p auth.Principal, id string) (db.Machine, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Machine{}, fmt.Errorf("starting revoke transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetMachine(ctx, db.GetMachineParams{OrgID: p.OrgID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Machine{}, ErrNotFound
	}
	if err != nil {
		return db.Machine{}, fmt.Errorf("loading machine: %w", err)
	}
	after, err := q.RevokeMachine(ctx, db.RevokeMachineParams{OrgID: p.OrgID, ID: id})
	if err != nil {
		return db.Machine{}, fmt.Errorf("revoking machine: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "machine.revoke",
		TargetKind: "machine",
		TargetID:   after.ID,
		Before:     map[string]any{"revoked_at": before.RevokedAt},
		After:      map[string]any{"revoked_at": after.RevokedAt},
	}); err != nil {
		return db.Machine{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Machine{}, fmt.Errorf("committing revoke: %w", err)
	}
	return after, nil
}
