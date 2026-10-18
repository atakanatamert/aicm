// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/dbtest"
)

func masterKey(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

func newStore(t *testing.T, pool *pgxpool.Pool, key []byte) *Builtin {
	t.Helper()
	store, err := NewBuiltin(pool, key)
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	return store
}

func TestRoundTrip(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	store := newStore(t, pool, masterKey(1))
	ctx := context.Background()

	const plaintext = "sk-test-abcdefgh"
	info, err := store.Put(ctx, orgID, plaintext)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if info.Last4 != "efgh" {
		t.Errorf("Last4 = %q, want %q", info.Last4, "efgh")
	}
	if info.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}

	got, err := store.Reveal(ctx, orgID, info.ID)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got != plaintext {
		t.Errorf("Reveal = %q, want %q", got, plaintext)
	}

	meta, err := store.Info(ctx, orgID, info.ID)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if meta.ID != info.ID || meta.Last4 != "efgh" {
		t.Errorf("Info = %+v, want id %q and last4 %q", meta, info.ID, "efgh")
	}

	short, err := store.Put(ctx, orgID, "short")
	if err != nil {
		t.Fatalf("Put short: %v", err)
	}
	if short.Last4 != "" {
		t.Errorf("short secret Last4 = %q, want empty", short.Last4)
	}

	replaced, err := store.Replace(ctx, orgID, info.ID, "sk-new-12345678")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if replaced.ID != info.ID {
		t.Errorf("Replace id = %q, want %q", replaced.ID, info.ID)
	}
	if replaced.Last4 != "5678" {
		t.Errorf("Replace Last4 = %q, want %q", replaced.Last4, "5678")
	}
	got, err = store.Reveal(ctx, orgID, info.ID)
	if err != nil {
		t.Fatalf("Reveal after Replace: %v", err)
	}
	if got != "sk-new-12345678" {
		t.Errorf("Reveal after Replace = %q, want %q", got, "sk-new-12345678")
	}
}

func TestCiphertextBoundToRow(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	store := newStore(t, pool, masterKey(2))
	ctx := context.Background()

	first, err := store.Put(ctx, orgID, "first-secret-value")
	if err != nil {
		t.Fatalf("Put first: %v", err)
	}
	second, err := store.Put(ctx, orgID, "second-secret-value")
	if err != nil {
		t.Fatalf("Put second: %v", err)
	}

	// Move the first secret's sealed bytes onto the second row. The
	// associated data names the secret id, so decryption must fail.
	if _, err := pool.Exec(ctx, `
		UPDATE secrets SET nonce = src.nonce, ciphertext = src.ciphertext
		FROM secrets AS src
		WHERE secrets.org_id = $1 AND secrets.id = $2
		  AND src.org_id = $1 AND src.id = $3`,
		orgID, second.ID, first.ID); err != nil {
		t.Fatalf("copying ciphertext: %v", err)
	}

	if _, err := store.Reveal(ctx, orgID, second.ID); err == nil {
		t.Fatal("Reveal of tampered row succeeded, want error")
	}
}

func TestWrongMasterKey(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	store := newStore(t, pool, masterKey(3))
	ctx := context.Background()

	info, err := store.Put(ctx, orgID, "sk-wrong-key-test")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	other := newStore(t, pool, masterKey(4))
	if _, err := other.Reveal(ctx, orgID, info.ID); !errors.Is(err, ErrWrongMasterKey) {
		t.Fatalf("Reveal with another master key: err = %v, want ErrWrongMasterKey", err)
	}
}

func TestRotateMasterKey(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	ctx := context.Background()
	oldKey := masterKey(5)
	newKey := masterKey(6)
	store := newStore(t, pool, oldKey)

	first, err := store.Put(ctx, orgID, "rotate-secret-one")
	if err != nil {
		t.Fatalf("Put first: %v", err)
	}
	second, err := store.Put(ctx, orgID, "rotate-secret-two")
	if err != nil {
		t.Fatalf("Put second: %v", err)
	}

	var before int32
	if err := pool.QueryRow(ctx, "SELECT max(master_key_version) FROM org_keys WHERE org_id = $1", orgID).Scan(&before); err != nil {
		t.Fatalf("reading master_key_version before: %v", err)
	}

	n, err := RotateMasterKey(ctx, pool, oldKey, newKey)
	if err != nil {
		t.Fatalf("RotateMasterKey: %v", err)
	}
	if n != 1 {
		t.Errorf("RotateMasterKey re-wrapped %d keys, want 1", n)
	}

	var after int32
	if err := pool.QueryRow(ctx, "SELECT max(master_key_version) FROM org_keys WHERE org_id = $1", orgID).Scan(&after); err != nil {
		t.Fatalf("reading master_key_version after: %v", err)
	}
	if after != before+1 {
		t.Errorf("master_key_version = %d, want %d", after, before+1)
	}

	newStore := newStore(t, pool, newKey)
	for _, info := range []Info{first, second} {
		if _, err := newStore.Reveal(ctx, orgID, info.ID); err != nil {
			t.Errorf("Reveal with new master key for %s: %v", info.ID, err)
		}
	}
	if _, err := store.Reveal(ctx, orgID, first.ID); !errors.Is(err, ErrWrongMasterKey) {
		t.Errorf("Reveal with old master key: err = %v, want ErrWrongMasterKey", err)
	}
}

func TestOrgIsolation(t *testing.T) {
	pool := dbtest.New(t)
	orgA := dbtest.DefaultOrgID(t, pool)
	ctx := context.Background()

	var orgB string
	if err := pool.QueryRow(ctx,
		"INSERT INTO orgs (slug, name) VALUES ('other', 'Other') RETURNING id").Scan(&orgB); err != nil {
		t.Fatalf("creating second org: %v", err)
	}

	store := newStore(t, pool, masterKey(7))
	info, err := store.Put(ctx, orgA, "org-a-only-secret")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := store.Reveal(ctx, orgB, info.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Reveal through org B: err = %v, want ErrNotFound", err)
	}
	if _, err := store.Replace(ctx, orgB, info.ID, "org-b-takeover"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Replace through org B: err = %v, want ErrNotFound", err)
	}
	if got, err := store.Reveal(ctx, orgA, info.ID); err != nil || got != "org-a-only-secret" {
		t.Errorf("Reveal through org A = %q, %v; want the original secret", got, err)
	}
}
