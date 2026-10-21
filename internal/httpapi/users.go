// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"strings"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
)

// ListApiTokens returns the caller's own API tokens.
func (s *Server) ListApiTokens(ctx context.Context, _ ListApiTokensRequestObject) (ListApiTokensResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Auth.ListAPITokens(ctx, p)
	if err != nil {
		return nil, mapUsersError(err)
	}
	out := make([]ApiToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIToken(row))
	}
	return ListApiTokens200JSONResponse(out), nil
}

// CreateApiToken creates a token and shows the raw value once.
func (s *Server) CreateApiToken(ctx context.Context, req CreateApiTokenRequestObject) (CreateApiTokenResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	name := strings.TrimSpace(req.Body.Name)
	if name == "" || len(name) > 100 {
		return nil, BadRequest("name must be between 1 and 100 characters")
	}
	row, raw, err := s.Auth.CreateAPIToken(ctx, p, name, req.Body.ExpiresAt)
	if err != nil {
		return nil, mapUsersError(err)
	}
	created := ApiTokenCreated{
		Id:         row.ID,
		Name:       row.Name,
		Last4:      row.Last4,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt,
		ExpiresAt:  row.ExpiresAt,
		RevokedAt:  row.RevokedAt,
		Token:      raw,
	}
	return CreateApiToken201JSONResponse(created), nil
}

// RevokeApiToken revokes one of the caller's own tokens.
func (s *Server) RevokeApiToken(ctx context.Context, req RevokeApiTokenRequestObject) (RevokeApiTokenResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Auth.RevokeAPIToken(ctx, p, req.TokenId); err != nil {
		return nil, mapUsersError(err)
	}
	return RevokeApiToken204Response{}, nil
}

// ListUsers returns the org's users.
func (s *Server) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Auth.ListUsers(ctx, p)
	if err != nil {
		return nil, mapUsersError(err)
	}
	out := make([]User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toUser(row))
	}
	return ListUsers200JSONResponse(out), nil
}

// CreateUser adds a user to the org.
func (s *Server) CreateUser(ctx context.Context, req CreateUserRequestObject) (CreateUserResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	role := auth.Role(req.Body.Role)
	if !role.Valid() {
		return nil, BadRequest("invalid role")
	}
	if err := auth.ValidatePassword(req.Body.Password); err != nil {
		return nil, BadRequest(err.Error())
	}
	if strings.TrimSpace(req.Body.Email) == "" {
		return nil, BadRequest("email is required")
	}
	name := ""
	if req.Body.Name != nil {
		name = *req.Body.Name
	}
	user, err := s.Auth.CreateUser(ctx, p, req.Body.Email, name, req.Body.Password, role)
	if err != nil {
		return nil, mapUsersError(err)
	}
	return CreateUser201JSONResponse(toUser(user)), nil
}

// UpdateUser changes a user's name, role, disabled state or password.
func (s *Server) UpdateUser(ctx context.Context, req UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	var update auth.UserUpdate
	if req.Body.Name != nil {
		update.Name = req.Body.Name
	}
	if req.Body.Role != nil {
		role := auth.Role(*req.Body.Role)
		if !role.Valid() {
			return nil, BadRequest("invalid role")
		}
		update.Role = &role
	}
	if req.Body.Disabled != nil {
		update.Disabled = req.Body.Disabled
	}
	if req.Body.Password != nil {
		if err := auth.ValidatePassword(*req.Body.Password); err != nil {
			return nil, BadRequest(err.Error())
		}
		update.Password = req.Body.Password
	}
	user, err := s.Auth.UpdateUser(ctx, p, req.UserId, update)
	if err != nil {
		return nil, mapUsersError(err)
	}
	return UpdateUser200JSONResponse(toUser(user)), nil
}

// ListGroups returns the org's groups with their members.
func (s *Server) ListGroups(ctx context.Context, _ ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := s.Auth.ListGroups(ctx, p)
	if err != nil {
		return nil, mapUsersError(err)
	}
	out := make([]Group, 0, len(groups))
	for _, group := range groups {
		out = append(out, toGroup(group))
	}
	return ListGroups200JSONResponse(out), nil
}

// CreateGroup adds a group.
func (s *Server) CreateGroup(ctx context.Context, req CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if strings.TrimSpace(req.Body.Name) == "" {
		return nil, BadRequest("name is required")
	}
	group, err := s.Auth.CreateGroup(ctx, p, req.Body.Name, req.Body.MemberIds)
	if err != nil {
		return nil, mapUsersError(err)
	}
	return CreateGroup201JSONResponse(toGroup(group)), nil
}

// UpdateGroup renames a group and replaces its members.
func (s *Server) UpdateGroup(ctx context.Context, req UpdateGroupRequestObject) (UpdateGroupResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if strings.TrimSpace(req.Body.Name) == "" {
		return nil, BadRequest("name is required")
	}
	group, err := s.Auth.UpdateGroup(ctx, p, req.GroupId, req.Body.Name, req.Body.MemberIds)
	if err != nil {
		return nil, mapUsersError(err)
	}
	return UpdateGroup200JSONResponse(toGroup(group)), nil
}

// DeleteGroup removes a group and its grants.
func (s *Server) DeleteGroup(ctx context.Context, req DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Auth.DeleteGroup(ctx, p, req.GroupId); err != nil {
		return nil, mapUsersError(err)
	}
	return DeleteGroup204Response{}, nil
}

// GetAccountGrants returns who can see an account besides admins.
func (s *Server) GetAccountGrants(ctx context.Context, req GetAccountGrantsRequestObject) (GetAccountGrantsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	grants, err := s.Auth.AccountGrants(ctx, p, req.AccountId)
	if err != nil {
		return nil, mapUsersError(err)
	}
	return GetAccountGrants200JSONResponse(toAccountGrants(grants)), nil
}

// SetAccountGrants replaces an account's grants.
func (s *Server) SetAccountGrants(ctx context.Context, req SetAccountGrantsRequestObject) (SetAccountGrantsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	grants, err := s.Auth.SetAccountGrants(ctx, p, req.AccountId, auth.Grants{
		UserIDs:  req.Body.UserIds,
		GroupIDs: req.Body.GroupIds,
	})
	if err != nil {
		return nil, mapUsersError(err)
	}
	return SetAccountGrants200JSONResponse(toAccountGrants(grants)), nil
}

func toUser(u db.User) User {
	return User{
		Id:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		Role:        Role(u.Role),
		Disabled:    u.DisabledAt != nil,
		TotpEnabled: new(u.TotpEnabledAt != nil),
		SsoLinked:   new(u.OidcSubject != nil),
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

func toAPIToken(t db.ApiToken) ApiToken {
	return ApiToken{
		Id:         t.ID,
		Name:       t.Name,
		Last4:      t.Last4,
		CreatedAt:  t.CreatedAt,
		LastUsedAt: t.LastUsedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
	}
}

func toGroup(g auth.Group) Group {
	members := g.MemberIDs
	if members == nil {
		members = []string{}
	}
	return Group{Id: g.ID, Name: g.Name, MemberIds: members}
}

func toAccountGrants(g auth.Grants) AccountGrants {
	users := g.UserIDs
	if users == nil {
		users = []string{}
	}
	groups := g.GroupIDs
	if groups == nil {
		groups = []string{}
	}
	return AccountGrants{UserIds: users, GroupIds: groups}
}

// mapUsersError turns user, group and grant sentinels into API errors.
func mapUsersError(err error) error {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return NotFound(err.Error())
	case errors.Is(err, auth.ErrEmailTaken),
		errors.Is(err, auth.ErrNameTaken),
		errors.Is(err, auth.ErrLastAdmin):
		return Conflict(err.Error())
	case errors.Is(err, auth.ErrUnauthenticated):
		return ErrUnauthorized
	case errors.Is(err, auth.ErrForbidden):
		return ErrForbidden
	}
	return err
}
