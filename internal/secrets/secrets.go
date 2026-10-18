// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secrets stores provider keys and other credentials encrypted at
// rest. Each org has a data key (DEK) that is wrapped by the master key;
// each secret is sealed with its org's DEK using XChaCha20-Poly1305.
//
// Plaintext never leaves this package. Callers get an Info value (id, last
// four characters, update time) or, right before use, Reveal.
package secrets

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/atakanatamert/aicm/internal/db"
)

const (
	// keySize is the length of the master key and of every org DEK.
	keySize = chacha20poly1305.KeySize
	// nonceSize is XChaCha20-Poly1305's nonce length.
	nonceSize = chacha20poly1305.NonceSizeX
)

// Store is the only way code reads or writes secrets. A future OpenBao or
// KMS backend implements the same interface.
type Store interface {
	// Put encrypts plaintext as a new secret in the org and returns its id.
	Put(ctx context.Context, orgID, plaintext string) (Info, error)
	// Replace re-encrypts an existing secret with a new value.
	Replace(ctx context.Context, orgID, secretID, plaintext string) (Info, error)
	// Reveal decrypts a secret. Call it only right before using the value
	// (sync workers, SMTP). Never log or return the result to a client.
	Reveal(ctx context.Context, orgID, secretID string) (string, error)
	// Info returns non-secret metadata.
	Info(ctx context.Context, orgID, secretID string) (Info, error)
	// Delete removes a secret.
	Delete(ctx context.Context, orgID, secretID string) error
}

// Info is safe to show: at most the last four characters.
type Info struct {
	ID        string
	Last4     string // "" when the plaintext is shorter than 8 characters
	UpdatedAt time.Time
}

// ErrNotFound is returned (wrapped) for an unknown secret id in the org.
var ErrNotFound = errors.New("secret not found")

// ErrWrongMasterKey means a data key could not be unwrapped: the master key
// is not the one the data was encrypted with.
var ErrWrongMasterKey = errors.New("master key does not match stored data keys")

// Builtin is the default Store: XChaCha20-Poly1305 envelope encryption in Postgres.
type Builtin struct {
	q         *db.Queries
	masterKey []byte
}

var _ Store = (*Builtin)(nil)

// NewBuiltin returns a store backed by pool that unwraps org data keys with
// masterKey.
func NewBuiltin(pool *pgxpool.Pool, masterKey []byte) (*Builtin, error) {
	if len(masterKey) != keySize {
		return nil, fmt.Errorf("master key must be %d bytes, got %d", keySize, len(masterKey))
	}
	key := make([]byte, keySize)
	copy(key, masterKey)
	return &Builtin{q: db.New(pool), masterKey: key}, nil
}

// Check unwraps the first org data key found, if any, and reports whether it
// belongs to the configured master key. It returns nil when no org key exists
// yet.
func (b *Builtin) Check(ctx context.Context) error {
	keys, err := b.q.ListOrgKeysForRotation(ctx)
	if err != nil {
		return fmt.Errorf("loading org keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	dek, err := unwrapKey(b.masterKey, keys[0])
	if err != nil {
		return err
	}
	clear(dek)
	return nil
}

// Put encrypts plaintext as a new secret in the org and returns its id.
func (b *Builtin) Put(ctx context.Context, orgID, plaintext string) (Info, error) {
	id, err := newUUID()
	if err != nil {
		return Info{}, fmt.Errorf("generating secret id: %w", err)
	}
	key, dek, err := b.activeKey(ctx, orgID)
	if err != nil {
		return Info{}, err
	}
	defer clear(dek)

	nonce, ciphertext, err := seal(dek, []byte(plaintext), secretAD(orgID, id))
	if err != nil {
		return Info{}, fmt.Errorf("encrypting secret: %w", err)
	}
	row, err := b.q.InsertSecret(ctx, db.InsertSecretParams{
		ID:         id,
		OrgID:      orgID,
		OrgKeyID:   key.ID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Last4:      last4(plaintext),
	})
	if err != nil {
		return Info{}, fmt.Errorf("inserting secret: %w", err)
	}
	return Info{ID: row.ID, Last4: row.Last4, UpdatedAt: row.UpdatedAt}, nil
}

// Replace re-encrypts an existing secret with a new value.
func (b *Builtin) Replace(ctx context.Context, orgID, secretID, plaintext string) (Info, error) {
	if _, err := b.q.GetSecret(ctx, db.GetSecretParams{OrgID: orgID, ID: secretID}); err != nil {
		return Info{}, notFoundOr(err, "loading secret", secretID)
	}
	key, dek, err := b.activeKey(ctx, orgID)
	if err != nil {
		return Info{}, err
	}
	defer clear(dek)

	nonce, ciphertext, err := seal(dek, []byte(plaintext), secretAD(orgID, secretID))
	if err != nil {
		return Info{}, fmt.Errorf("encrypting secret: %w", err)
	}
	row, err := b.q.UpdateSecret(ctx, db.UpdateSecretParams{
		OrgID:      orgID,
		ID:         secretID,
		OrgKeyID:   key.ID,
		Nonce:      nonce,
		Ciphertext: ciphertext,
		Last4:      last4(plaintext),
	})
	if err != nil {
		return Info{}, notFoundOr(err, "updating secret", secretID)
	}
	return Info{ID: row.ID, Last4: row.Last4, UpdatedAt: row.UpdatedAt}, nil
}

// Reveal decrypts a secret. Call it only right before using the value.
func (b *Builtin) Reveal(ctx context.Context, orgID, secretID string) (string, error) {
	row, err := b.q.GetSecret(ctx, db.GetSecretParams{OrgID: orgID, ID: secretID})
	if err != nil {
		return "", notFoundOr(err, "loading secret", secretID)
	}
	key, err := b.q.GetOrgKey(ctx, db.GetOrgKeyParams{OrgID: orgID, ID: row.OrgKeyID})
	if err != nil {
		return "", fmt.Errorf("loading org key: %w", err)
	}
	dek, err := unwrapKey(b.masterKey, key)
	if err != nil {
		return "", err
	}
	defer clear(dek)

	aead, err := chacha20poly1305.NewX(dek)
	if err != nil {
		return "", fmt.Errorf("decrypting secret %s: %w", secretID, err)
	}
	plaintext, err := aead.Open(nil, row.Nonce, row.Ciphertext, secretAD(orgID, secretID))
	if err != nil {
		return "", fmt.Errorf("decrypting secret %s: authentication failed", secretID)
	}
	return string(plaintext), nil
}

// Info returns non-secret metadata.
func (b *Builtin) Info(ctx context.Context, orgID, secretID string) (Info, error) {
	row, err := b.q.GetSecret(ctx, db.GetSecretParams{OrgID: orgID, ID: secretID})
	if err != nil {
		return Info{}, notFoundOr(err, "loading secret", secretID)
	}
	return Info{ID: row.ID, Last4: row.Last4, UpdatedAt: row.UpdatedAt}, nil
}

// Delete removes a secret.
func (b *Builtin) Delete(ctx context.Context, orgID, secretID string) error {
	n, err := b.q.DeleteSecret(ctx, db.DeleteSecretParams{OrgID: orgID, ID: secretID})
	if err != nil {
		return fmt.Errorf("deleting secret %s: %w", secretID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, secretID)
	}
	return nil
}

// activeKey returns the org's active data key, creating version 1 on first
// use. Concurrent first Puts do not create two keys: the losing insert
// conflicts with the winner's row and is followed by a select.
func (b *Builtin) activeKey(ctx context.Context, orgID string) (db.OrgKey, []byte, error) {
	key, err := b.q.GetActiveOrgKey(ctx, orgID)
	switch {
	case err == nil:
		dek, err := unwrapKey(b.masterKey, key)
		if err != nil {
			return db.OrgKey{}, nil, err
		}
		return key, dek, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return db.OrgKey{}, nil, fmt.Errorf("loading active org key: %w", err)
	}

	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return db.OrgKey{}, nil, fmt.Errorf("generating org data key: %w", err)
	}
	wrapped, err := wrapKey(b.masterKey, orgID, 1, dek)
	if err != nil {
		clear(dek)
		return db.OrgKey{}, nil, err
	}
	key, err = b.q.InsertOrgKey(ctx, db.InsertOrgKeyParams{
		OrgID:            orgID,
		Version:          1,
		WrappedKey:       wrapped,
		MasterKeyVersion: 1,
	})
	if err == nil {
		return key, dek, nil
	}
	clear(dek)
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.OrgKey{}, nil, fmt.Errorf("inserting org key: %w", err)
	}

	// Another writer created the key first; use theirs.
	key, err = b.q.GetActiveOrgKey(ctx, orgID)
	if err != nil {
		return db.OrgKey{}, nil, fmt.Errorf("loading active org key after conflict: %w", err)
	}
	dek, err = unwrapKey(b.masterKey, key)
	if err != nil {
		return db.OrgKey{}, nil, err
	}
	return key, dek, nil
}

// RotateMasterKey re-wraps every org data key (all orgs) from oldKey to
// newKey in one transaction and sets master_key_version to the previous
// maximum + 1. It returns the number of data keys re-wrapped. Secrets
// themselves are not re-encrypted (their DEKs don't change).
func RotateMasterKey(ctx context.Context, pool *pgxpool.Pool, oldKey, newKey []byte) (int, error) {
	if len(oldKey) != keySize {
		return 0, fmt.Errorf("old master key must be %d bytes, got %d", keySize, len(oldKey))
	}
	if len(newKey) != keySize {
		return 0, fmt.Errorf("new master key must be %d bytes, got %d", keySize, len(newKey))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("starting rotation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := db.New(tx)
	keys, err := q.ListOrgKeysForRotation(ctx)
	if err != nil {
		return 0, fmt.Errorf("loading org keys: %w", err)
	}
	var maxVersion int32
	for _, key := range keys {
		if key.MasterKeyVersion > maxVersion {
			maxVersion = key.MasterKeyVersion
		}
	}
	newVersion := maxVersion + 1

	for _, key := range keys {
		dek, err := unwrapKey(oldKey, key)
		if err != nil {
			return 0, err
		}
		wrapped, err := wrapKey(newKey, key.OrgID, key.Version, dek)
		clear(dek)
		if err != nil {
			return 0, err
		}
		if err := q.UpdateOrgKeyWrap(ctx, db.UpdateOrgKeyWrapParams{
			OrgID:            key.OrgID,
			ID:               key.ID,
			WrappedKey:       wrapped,
			MasterKeyVersion: newVersion,
		}); err != nil {
			return 0, fmt.Errorf("updating org key %s: %w", key.ID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing rotation: %w", err)
	}
	return len(keys), nil
}

// unwrapKey authenticates and decrypts an org data key with the master key.
func unwrapKey(masterKey []byte, key db.OrgKey) ([]byte, error) {
	if len(key.WrappedKey) < nonceSize {
		return nil, fmt.Errorf("%w: org key %s is truncated", ErrWrongMasterKey, key.ID)
	}
	aead, err := chacha20poly1305.NewX(masterKey)
	if err != nil {
		return nil, fmt.Errorf("%w: unwrapping org key %s: %w", ErrWrongMasterKey, key.ID, err)
	}
	dek, err := aead.Open(nil, key.WrappedKey[:nonceSize], key.WrappedKey[nonceSize:], orgKeyAD(key.OrgID, key.Version))
	if err != nil {
		return nil, fmt.Errorf("%w: unwrapping org key %s: %w", ErrWrongMasterKey, key.ID, err)
	}
	if len(dek) != keySize {
		return nil, fmt.Errorf("org key %s has invalid length %d", key.ID, len(dek))
	}
	return dek, nil
}

// wrapKey seals dek with the master key. The result is nonce || ciphertext.
func wrapKey(masterKey []byte, orgID string, version int32, dek []byte) ([]byte, error) {
	nonce, ciphertext, err := seal(masterKey, dek, orgKeyAD(orgID, version))
	if err != nil {
		return nil, fmt.Errorf("wrapping org key: %w", err)
	}
	wrapped := make([]byte, 0, len(nonce)+len(ciphertext))
	wrapped = append(wrapped, nonce...)
	return append(wrapped, ciphertext...), nil
}

// seal encrypts plaintext with key and a fresh random nonce.
func seal(key, plaintext, ad []byte) (nonce, ciphertext []byte, err error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating cipher: %w", err)
	}
	nonce = make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generating nonce: %w", err)
	}
	return nonce, aead.Seal(nil, nonce, plaintext, ad), nil
}

func orgKeyAD(orgID string, version int32) []byte {
	return []byte("aicm/org-key/v1:" + orgID + ":" + strconv.FormatInt(int64(version), 10))
}

func secretAD(orgID, secretID string) []byte {
	return []byte("aicm/secret/v1:" + orgID + ":" + secretID)
}

// last4 keeps the tail of long-enough plaintexts and hides short ones.
func last4(plaintext string) string {
	runes := []rune(plaintext)
	if len(runes) < 8 {
		return ""
	}
	return string(runes[len(runes)-4:])
}

// newUUID returns a random UUIDv4 in the canonical lowercase form.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func notFoundOr(err error, action, secretID string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, secretID)
	}
	return fmt.Errorf("%s %s: %w", action, secretID, err)
}
