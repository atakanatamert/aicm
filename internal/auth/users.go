// SPDX-License-Identifier: SUL-1.0

package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/db"
)

// Errors returned by the user, group, grant and token methods.
var (
	ErrNotFound   = errors.New("not found")
	ErrEmailTaken = errors.New("email already in use")
	ErrNameTaken  = errors.New("name already in use")
	ErrLastAdmin  = errors.New("the last admin cannot be demoted or disabled")
)

// UserUpdate describes the fields changeable through the admin API. Nil
// fields are left alone.
type UserUpdate struct {
	Name     *string
	Role     *Role
	Disabled *bool
	Password *string
}

// Group is a named set of users used for account grants.
type Group struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	MemberIDs []string `json:"member_ids"`
}

// Grants is who may see an account besides admins.
type Grants struct {
	UserIDs  []string `json:"user_ids"`
	GroupIDs []string `json:"group_ids"`
}

// ListAPITokens returns the principal's own tokens.
func (s *Service) ListAPITokens(ctx context.Context, p Principal) ([]db.ApiToken, error) {
	if p.UserID == "" {
		return nil, ErrUnauthenticated
	}
	rows, err := s.queries.ListAPITokens(ctx, db.ListAPITokensParams{OrgID: p.OrgID, UserID: p.UserID})
	if err != nil {
		return nil, fmt.Errorf("listing API tokens: %w", err)
	}
	return rows, nil
}

// CreateAPIToken returns the row and the raw token, shown once.
func (s *Service) CreateAPIToken(ctx context.Context, p Principal, name string, expiresAt *time.Time) (db.ApiToken, string, error) {
	if p.UserID == "" {
		return db.ApiToken{}, "", ErrUnauthenticated
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return db.ApiToken{}, "", errors.New("name is required")
	}
	if len(name) > 100 {
		return db.ApiToken{}, "", errors.New("name must be at most 100 characters")
	}

	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return db.ApiToken{}, "", fmt.Errorf("generating API token: %w", err)
	}
	raw := apiTokenPrefix + base64.RawURLEncoding.EncodeToString(key[:])

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.ApiToken{}, "", fmt.Errorf("starting API token transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	row, err := q.InsertAPIToken(ctx, db.InsertAPITokenParams{
		OrgID:     p.OrgID,
		UserID:    p.UserID,
		Name:      name,
		TokenHash: hashToken(raw),
		Last4:     raw[len(raw)-4:],
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return db.ApiToken{}, "", fmt.Errorf("creating API token: %w", err)
	}
	if err := s.auditChange(ctx, q, p, "api_token.create", "api_token", row.ID, nil, map[string]any{
		"id":         row.ID,
		"name":       row.Name,
		"last4":      row.Last4,
		"expires_at": row.ExpiresAt,
	}); err != nil {
		return db.ApiToken{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ApiToken{}, "", fmt.Errorf("committing API token: %w", err)
	}
	return row, raw, nil
}

// RevokeAPIToken revokes one of the principal's own tokens.
func (s *Service) RevokeAPIToken(ctx context.Context, p Principal, tokenID string) error {
	if p.UserID == "" {
		return ErrUnauthenticated
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting revoke transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	row, err := q.RevokeAPIToken(ctx, db.RevokeAPITokenParams{OrgID: p.OrgID, UserID: p.UserID, ID: tokenID})
	if err != nil {
		if isMissing(err) {
			return ErrNotFound
		}
		return fmt.Errorf("revoking API token: %w", err)
	}
	if err := s.auditChange(ctx, q, p, "api_token.revoke", "api_token", row.ID, nil, map[string]any{
		"revoked_at": row.RevokedAt,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing revoke: %w", err)
	}
	return nil
}

// ListUsers returns every user in the principal's org.
func (s *Service) ListUsers(ctx context.Context, p Principal) ([]db.User, error) {
	rows, err := s.queries.ListUsers(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return rows, nil
}

// CreateUser adds a user to the principal's org.
func (s *Service) CreateUser(ctx context.Context, p Principal, email, name, password string, role Role) (db.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return db.User{}, errors.New("email is required")
	}
	if !role.Valid() {
		return db.User{}, errors.New("invalid role")
	}
	if err := ValidatePassword(password); err != nil {
		return db.User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return db.User{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, fmt.Errorf("starting user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	user, err := q.InsertUser(ctx, db.InsertUserParams{
		OrgID:        p.OrgID,
		Email:        email,
		Name:         name,
		PasswordHash: hash,
		Role:         string(role),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return db.User{}, ErrEmailTaken
		}
		return db.User{}, fmt.Errorf("creating user: %w", err)
	}
	if err := s.auditChange(ctx, q, p, "user.create", "user", user.ID, nil, map[string]any{
		"id":       user.ID,
		"email":    user.Email,
		"name":     user.Name,
		"role":     user.Role,
		"disabled": user.DisabledAt != nil,
	}); err != nil {
		return db.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, fmt.Errorf("committing user: %w", err)
	}
	return user, nil
}

// UpdateUser changes name, role, disabled state or password. The last enabled
// admin cannot be demoted or disabled.
func (s *Service) UpdateUser(ctx context.Context, p Principal, userID string, u UserUpdate) (db.User, error) {
	if u.Role != nil && !u.Role.Valid() {
		return db.User{}, errors.New("invalid role")
	}
	if u.Password != nil {
		if err := ValidatePassword(*u.Password); err != nil {
			return db.User{}, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.User{}, fmt.Errorf("starting user update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	current, err := q.GetUserByID(ctx, db.GetUserByIDParams{OrgID: p.OrgID, ID: userID})
	if err != nil {
		if isMissing(err) {
			return db.User{}, ErrNotFound
		}
		return db.User{}, fmt.Errorf("loading user: %w", err)
	}

	wasDisabled := current.DisabledAt != nil
	role := current.Role
	if u.Role != nil {
		role = string(*u.Role)
	}
	name := current.Name
	if u.Name != nil {
		name = *u.Name
	}
	disabledAt := current.DisabledAt
	if u.Disabled != nil {
		if *u.Disabled {
			if disabledAt == nil {
				now := time.Now().UTC()
				disabledAt = &now
			}
		} else {
			disabledAt = nil
		}
	}

	if Role(current.Role) == RoleAdmin && !wasDisabled &&
		(Role(role) != RoleAdmin || disabledAt != nil) {
		admins, err := q.CountEnabledAdmins(ctx, p.OrgID)
		if err != nil {
			return db.User{}, fmt.Errorf("counting admins: %w", err)
		}
		if admins <= 1 {
			return db.User{}, ErrLastAdmin
		}
	}

	passwordHash := current.PasswordHash
	if u.Password != nil {
		hash, err := HashPassword(*u.Password)
		if err != nil {
			return db.User{}, err
		}
		passwordHash = hash
	}

	updated, err := q.UpdateUser(ctx, db.UpdateUserParams{
		OrgID:        p.OrgID,
		ID:           userID,
		Name:         name,
		Role:         role,
		DisabledAt:   disabledAt,
		PasswordHash: passwordHash,
	})
	if err != nil {
		if isMissing(err) {
			return db.User{}, ErrNotFound
		}
		return db.User{}, fmt.Errorf("updating user: %w", err)
	}

	// A disabled account or a password reset ends every existing session.
	if u.Password != nil || (u.Disabled != nil && *u.Disabled) {
		if err := q.DeleteSessionsForUser(ctx, db.DeleteSessionsForUserParams{OrgID: p.OrgID, UserID: userID}); err != nil {
			return db.User{}, fmt.Errorf("deleting user sessions: %w", err)
		}
	}

	before := map[string]any{}
	after := map[string]any{}
	if u.Name != nil && *u.Name != current.Name {
		before["name"] = current.Name
		after["name"] = *u.Name
	}
	if u.Role != nil && string(*u.Role) != current.Role {
		before["role"] = current.Role
		after["role"] = string(*u.Role)
	}
	if u.Disabled != nil && *u.Disabled != wasDisabled {
		before["disabled"] = wasDisabled
		after["disabled"] = *u.Disabled
	}
	if u.Password != nil {
		after["password"] = "[redacted]"
	}
	if err := s.auditChange(ctx, q, p, "user.update", "user", userID, before, after); err != nil {
		return db.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.User{}, fmt.Errorf("committing user update: %w", err)
	}
	return updated, nil
}

// ListGroups returns every group in the org with its members.
func (s *Service) ListGroups(ctx context.Context, p Principal) ([]Group, error) {
	rows, err := s.queries.ListGroups(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	memberships, err := s.queries.ListGroupMemberships(ctx, p.OrgID)
	if err != nil {
		return nil, fmt.Errorf("listing group memberships: %w", err)
	}
	byGroup := make(map[string][]string, len(rows))
	for _, m := range memberships {
		byGroup[m.GroupID] = append(byGroup[m.GroupID], m.UserID)
	}
	groups := make([]Group, 0, len(rows))
	for _, g := range rows {
		ids := byGroup[g.ID]
		if ids == nil {
			ids = []string{}
		}
		groups = append(groups, Group{ID: g.ID, Name: g.Name, MemberIDs: ids})
	}
	return groups, nil
}

// CreateGroup adds a group. Members must belong to the principal's org.
func (s *Service) CreateGroup(ctx context.Context, p Principal, name string, memberIDs []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("name is required")
	}
	members := cleanIDs(memberIDs)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("starting group transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	row, err := q.InsertGroup(ctx, db.InsertGroupParams{OrgID: p.OrgID, Name: name})
	if err != nil {
		if isUniqueViolation(err) {
			return Group{}, ErrNameTaken
		}
		return Group{}, fmt.Errorf("creating group: %w", err)
	}
	for _, userID := range members {
		if err := q.InsertGroupMember(ctx, db.InsertGroupMemberParams{
			OrgID: p.OrgID, GroupID: row.ID, UserID: userID,
		}); err != nil {
			if isMissing(err) {
				return Group{}, ErrNotFound
			}
			return Group{}, fmt.Errorf("adding group member: %w", err)
		}
	}
	group := Group{ID: row.ID, Name: row.Name, MemberIDs: members}
	if err := s.auditChange(ctx, q, p, "group.create", "group", group.ID, nil, group); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("committing group: %w", err)
	}
	return group, nil
}

// UpdateGroup renames a group and replaces its members.
func (s *Service) UpdateGroup(ctx context.Context, p Principal, groupID, name string, memberIDs []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("name is required")
	}
	members := cleanIDs(memberIDs)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("starting group update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	current, err := q.GetGroup(ctx, db.GetGroupParams{OrgID: p.OrgID, ID: groupID})
	if err != nil {
		if isMissing(err) {
			return Group{}, ErrNotFound
		}
		return Group{}, fmt.Errorf("loading group: %w", err)
	}
	beforeMembers, err := q.ListGroupMembers(ctx, db.ListGroupMembersParams{OrgID: p.OrgID, GroupID: groupID})
	if err != nil {
		return Group{}, fmt.Errorf("listing group members: %w", err)
	}

	updated, err := q.UpdateGroupName(ctx, db.UpdateGroupNameParams{OrgID: p.OrgID, ID: groupID, Name: name})
	if err != nil {
		if isUniqueViolation(err) {
			return Group{}, ErrNameTaken
		}
		return Group{}, fmt.Errorf("renaming group: %w", err)
	}
	if err := q.DeleteGroupMembers(ctx, db.DeleteGroupMembersParams{OrgID: p.OrgID, GroupID: groupID}); err != nil {
		return Group{}, fmt.Errorf("replacing group members: %w", err)
	}
	for _, userID := range members {
		if err := q.InsertGroupMember(ctx, db.InsertGroupMemberParams{
			OrgID: p.OrgID, GroupID: groupID, UserID: userID,
		}); err != nil {
			if isMissing(err) {
				return Group{}, ErrNotFound
			}
			return Group{}, fmt.Errorf("adding group member: %w", err)
		}
	}

	after := Group{ID: updated.ID, Name: updated.Name, MemberIDs: members}
	before := Group{ID: current.ID, Name: current.Name, MemberIDs: beforeMembers}
	if err := s.auditChange(ctx, q, p, "group.update", "group", groupID, before, after); err != nil {
		return Group{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("committing group update: %w", err)
	}
	return after, nil
}

// DeleteGroup removes a group and, by cascade, its grants.
func (s *Service) DeleteGroup(ctx context.Context, p Principal, groupID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting group delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	current, err := q.GetGroup(ctx, db.GetGroupParams{OrgID: p.OrgID, ID: groupID})
	if err != nil {
		if isMissing(err) {
			return ErrNotFound
		}
		return fmt.Errorf("loading group: %w", err)
	}
	members, err := q.ListGroupMembers(ctx, db.ListGroupMembersParams{OrgID: p.OrgID, GroupID: groupID})
	if err != nil {
		return fmt.Errorf("listing group members: %w", err)
	}
	if _, err := q.DeleteGroup(ctx, db.DeleteGroupParams{OrgID: p.OrgID, ID: groupID}); err != nil {
		if isMissing(err) {
			return ErrNotFound
		}
		return fmt.Errorf("deleting group: %w", err)
	}
	before := Group{ID: current.ID, Name: current.Name, MemberIDs: members}
	if err := s.auditChange(ctx, q, p, "group.delete", "group", groupID, before, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing group delete: %w", err)
	}
	return nil
}

// AccountGrants returns the account's grants.
func (s *Service) AccountGrants(ctx context.Context, p Principal, accountID string) (Grants, error) {
	if _, err := s.queries.GetAccountByID(ctx, db.GetAccountByIDParams{OrgID: p.OrgID, ID: accountID}); err != nil {
		if isMissing(err) {
			return Grants{}, ErrNotFound
		}
		return Grants{}, fmt.Errorf("loading account: %w", err)
	}
	rows, err := s.queries.ListAccountGrants(ctx, db.ListAccountGrantsParams{OrgID: p.OrgID, AccountID: accountID})
	if err != nil {
		return Grants{}, fmt.Errorf("listing account grants: %w", err)
	}
	return grantsFromRows(rows), nil
}

// SetAccountGrants replaces the account's grants. Every referenced user,
// group and the account itself must belong to the principal's org.
func (s *Service) SetAccountGrants(ctx context.Context, p Principal, accountID string, g Grants) (Grants, error) {
	userIDs := cleanIDs(g.UserIDs)
	groupIDs := cleanIDs(g.GroupIDs)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Grants{}, fmt.Errorf("starting grants transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.queries.WithTx(tx)

	if _, err := q.GetAccountByID(ctx, db.GetAccountByIDParams{OrgID: p.OrgID, ID: accountID}); err != nil {
		if isMissing(err) {
			return Grants{}, ErrNotFound
		}
		return Grants{}, fmt.Errorf("loading account: %w", err)
	}
	beforeRows, err := q.ListAccountGrants(ctx, db.ListAccountGrantsParams{OrgID: p.OrgID, AccountID: accountID})
	if err != nil {
		return Grants{}, fmt.Errorf("listing account grants: %w", err)
	}
	before := grantsFromRows(beforeRows)

	if err := q.DeleteAccountGrants(ctx, db.DeleteAccountGrantsParams{OrgID: p.OrgID, AccountID: accountID}); err != nil {
		return Grants{}, fmt.Errorf("clearing account grants: %w", err)
	}
	for _, userID := range userIDs {
		if err := q.InsertAccountGrant(ctx, db.InsertAccountGrantParams{
			OrgID: p.OrgID, AccountID: accountID, UserID: new(userID),
		}); err != nil {
			if isMissing(err) {
				return Grants{}, ErrNotFound
			}
			return Grants{}, fmt.Errorf("granting account to user: %w", err)
		}
	}
	for _, groupID := range groupIDs {
		if err := q.InsertAccountGrant(ctx, db.InsertAccountGrantParams{
			OrgID: p.OrgID, AccountID: accountID, GroupID: new(groupID),
		}); err != nil {
			if isMissing(err) {
				return Grants{}, ErrNotFound
			}
			return Grants{}, fmt.Errorf("granting account to group: %w", err)
		}
	}

	after := Grants{UserIDs: userIDs, GroupIDs: groupIDs}
	if err := s.auditChange(ctx, q, p, "account_grants.set", "account", accountID, before, after); err != nil {
		return Grants{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Grants{}, fmt.Errorf("committing grants: %w", err)
	}
	return after, nil
}

// grantsFromRows splits grant rows into user and group ids.
func grantsFromRows(rows []db.AccountGrant) Grants {
	g := Grants{UserIDs: []string{}, GroupIDs: []string{}}
	for _, row := range rows {
		if row.UserID != nil {
			g.UserIDs = append(g.UserIDs, *row.UserID)
		}
		if row.GroupID != nil {
			g.GroupIDs = append(g.GroupIDs, *row.GroupID)
		}
	}
	sort.Strings(g.UserIDs)
	sort.Strings(g.GroupIDs)
	return g
}

// cleanIDs copies ids, removes duplicates and sorts them.
func cleanIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// isMissing reports a lookup that matched no row, including a malformed uuid.
func isMissing(err error) bool {
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "22P02" || pgErr.Code == "23503")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// auditChange writes one audit row in the caller's transaction.
func (s *Service) auditChange(ctx context.Context, q *db.Queries, p Principal, action, targetKind, targetID string, before, after any) error {
	return audit.Write(ctx, q, p.Actor(), audit.Entry{Action: action, TargetKind: targetKind, TargetID: targetID, Before: before, After: after}) //nolint:wrapcheck // audit.Write already wraps
}
