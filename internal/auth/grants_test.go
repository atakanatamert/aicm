// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/dbtest"
)

const testPassword = "correct horse battery staple"

func TestGrantVisibility(t *testing.T) {
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	svc := testService(pool)
	ctx := context.Background()

	admin, err := svc.CreateUser(ctx, Principal{OrgID: orgID, Role: RoleAdmin}, "admin@example.com", "Admin", testPassword, RoleAdmin)
	if err != nil {
		t.Fatalf("creating admin: %v", err)
	}
	p := Principal{OrgID: orgID, UserID: admin.ID, Role: RoleAdmin, Kind: ActorUser, ActorID: admin.ID, Label: admin.Email}
	viewer, err := svc.CreateUser(ctx, p, "viewer@example.com", "Viewer", testPassword, RoleViewer)
	if err != nil {
		t.Fatalf("creating viewer: %v", err)
	}

	group, err := svc.CreateGroup(ctx, p, "viewers", []string{viewer.ID})
	if err != nil {
		t.Fatalf("creating group: %v", err)
	}
	first := insertAccount(t, pool, orgID, "first")
	second := insertAccount(t, pool, orgID, "second")

	if !visible(t, pool, orgID, admin.ID, true, first) || !visible(t, pool, orgID, admin.ID, true, second) {
		t.Fatal("admin must see both accounts without grants")
	}
	if visible(t, pool, orgID, viewer.ID, false, first) || visible(t, pool, orgID, viewer.ID, false, second) {
		t.Fatal("viewer must see neither account without a grant")
	}

	if _, err := svc.SetAccountGrants(ctx, p, first, Grants{UserIDs: []string{viewer.ID}}); err != nil {
		t.Fatalf("granting first account directly: %v", err)
	}
	if !visible(t, pool, orgID, viewer.ID, false, first) {
		t.Fatal("direct user grant must make the account visible")
	}
	if visible(t, pool, orgID, viewer.ID, false, second) {
		t.Fatal("direct grant must not leak to another account")
	}

	if _, err := svc.SetAccountGrants(ctx, p, second, Grants{GroupIDs: []string{group.ID}}); err != nil {
		t.Fatalf("granting second account to group: %v", err)
	}
	if !visible(t, pool, orgID, viewer.ID, false, second) {
		t.Fatal("group grant must make the account visible to a member")
	}

	if _, err := svc.UpdateGroup(ctx, p, group.ID, group.Name, nil); err != nil {
		t.Fatalf("removing viewer from group: %v", err)
	}
	if visible(t, pool, orgID, viewer.ID, false, second) {
		t.Fatal("removing the user from the group must hide the account")
	}

	if _, err := svc.SetAccountGrants(ctx, p, first, Grants{}); err != nil {
		t.Fatalf("clearing first account grants: %v", err)
	}
	if visible(t, pool, orgID, viewer.ID, false, first) {
		t.Fatal("empty grants must hide the account")
	}

	otherOrg := insertOrg(t, pool, "other")
	otherAccount := insertAccount(t, pool, otherOrg, "other")
	otherGroup := insertGroup(t, pool, otherOrg, "other-group")
	if _, err := pool.Exec(ctx,
		`INSERT INTO account_grants (org_id, account_id, group_id) VALUES ($1, $2, $3)`,
		otherOrg, otherAccount, otherGroup,
	); err != nil {
		t.Fatalf("inserting cross-org grant: %v", err)
	}
	if visible(t, pool, orgID, viewer.ID, false, otherAccount) {
		t.Fatal("a grant in another org must never make an account visible")
	}
}

func TestLastAdmin(t *testing.T) {
	pool := dbtest.New(t)
	svc := testService(pool)
	ctx := context.Background()

	setup, err := svc.CompleteSetup(ctx, "admin@example.com", "Admin", testPassword, "test", netip.Addr{})
	if err != nil {
		t.Fatalf("completing setup: %v", err)
	}
	p := setup.Principal

	if _, err := svc.UpdateUser(ctx, p, p.UserID, UserUpdate{Role: new(RoleViewer)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the only admin: got %v, want ErrLastAdmin", err)
	}
	if _, err := svc.UpdateUser(ctx, p, p.UserID, UserUpdate{Disabled: new(true)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disabling the only admin: got %v, want ErrLastAdmin", err)
	}
	still, _, _, err := svc.Me(ctx, p)
	if err != nil {
		t.Fatalf("loading admin: %v", err)
	}
	if still.Role != string(RoleAdmin) || still.DisabledAt != nil {
		t.Fatal("refused update must leave the last admin enabled")
	}

	second, err := svc.CreateUser(ctx, p, "second@example.com", "Second", testPassword, RoleAdmin)
	if err != nil {
		t.Fatalf("creating second admin: %v", err)
	}
	updated, err := svc.UpdateUser(ctx, p, p.UserID, UserUpdate{Role: new(RoleViewer)})
	if err != nil {
		t.Fatalf("demoting with a second admin: %v", err)
	}
	if updated.Role != string(RoleViewer) {
		t.Fatalf("demoted role = %q, want viewer", updated.Role)
	}

	if _, err := svc.UpdateUser(ctx, p, second.ID, UserUpdate{Disabled: new(true)}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disabling the remaining admin: got %v, want ErrLastAdmin", err)
	}
	if _, err := svc.UpdateUser(ctx, p, p.UserID, UserUpdate{Role: new(RoleAdmin)}); err != nil {
		t.Fatalf("restoring first admin: %v", err)
	}
	disabled, err := svc.UpdateUser(ctx, p, second.ID, UserUpdate{Disabled: new(true)})
	if err != nil {
		t.Fatalf("disabling second admin with another admin present: %v", err)
	}
	if disabled.DisabledAt == nil {
		t.Fatal("second admin was not disabled")
	}
}

func testService(pool *pgxpool.Pool) *Service {
	return NewService(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), "")
}

func visible(t *testing.T, pool *pgxpool.Pool, orgID, userID string, isAdmin bool, accountID string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT account_visible($1, $2, $3, $4)`, orgID, userID, isAdmin, accountID,
	).Scan(&ok); err != nil {
		t.Fatalf("checking visibility: %v", err)
	}
	return ok
}

func insertAccount(t *testing.T, pool *pgxpool.Pool, orgID, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency)
		 VALUES ($1, 'test', $2, 'none', 'USD') RETURNING id`, orgID, name,
	).Scan(&id); err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	return id
}

func insertOrg(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO orgs (slug, name) VALUES ($1, $1) RETURNING id`, slug,
	).Scan(&id); err != nil {
		t.Fatalf("inserting org: %v", err)
	}
	return id
}

func insertGroup(t *testing.T, pool *pgxpool.Pool, orgID, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, orgID, name,
	).Scan(&id); err != nil {
		t.Fatalf("inserting group: %v", err)
	}
	return id
}
