// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/machine"
)

// ListEnrollTokens returns the org's enrollment tokens. Hashes are never
// returned.
func (s *Server) ListEnrollTokens(ctx context.Context, _ ListEnrollTokensRequestObject) (ListEnrollTokensResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Machines.ListEnrollTokens(ctx, p)
	if err != nil {
		return nil, mapMachineError(err)
	}
	out := make([]EnrollToken, 0, len(rows))
	for _, row := range rows {
		out = append(out, toEnrollToken(row))
	}
	return ListEnrollTokens200JSONResponse(out), nil
}

// CreateEnrollToken creates a universal enrollment token and shows the token
// and install commands once.
func (s *Server) CreateEnrollToken(ctx context.Context, req CreateEnrollTokenRequestObject) (CreateEnrollTokenResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	permanent := req.Body.Permanent != nil && *req.Body.Permanent
	ttl := 60 * time.Minute
	if req.Body.TtlMinutes != nil {
		ttl = time.Duration(*req.Body.TtlMinutes) * time.Minute
	}
	if !permanent && (ttl < 5*time.Minute || ttl > 10080*time.Minute) {
		return nil, BadRequest("ttl_minutes must be between 5 and 10080")
	}
	created, err := s.Machines.CreateEnrollToken(ctx, p, permanent, ttl)
	if err != nil {
		return nil, mapMachineError(err)
	}
	brew := created.Commands.Brew
	out := EnrollTokenCreated{
		Id:           created.Token.ID,
		CreatedAt:    created.Token.CreatedAt,
		ExpiresAt:    created.Token.ExpiresAt,
		RevokedAt:    created.Token.RevokedAt,
		LastUsedAt:   created.Token.LastUsedAt,
		UseCount:     int(created.Token.UseCount),
		Token:        created.Raw,
		HubUrl:       created.HubURL,
		OrgId:        created.OrgID,
		HubPublicKey: created.HubPublicKey,
	}
	out.Commands.Sh = created.Commands.Sh
	out.Commands.Brew = &brew
	out.Commands.Powershell = created.Commands.PowerShell
	out.Commands.Docker = created.Commands.Docker
	return CreateEnrollToken201JSONResponse(out), nil
}

// RevokeEnrollToken revokes one enrollment token. Enrolled machines keep
// their credentials.
func (s *Server) RevokeEnrollToken(ctx context.Context, req RevokeEnrollTokenRequestObject) (RevokeEnrollTokenResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Machines.RevokeEnrollToken(ctx, p, req.EnrollTokenId); err != nil {
		if errors.Is(err, machine.ErrNotFound) {
			return nil, NotFound("enroll token not found")
		}
		return nil, err
	}
	return RevokeEnrollToken204Response{}, nil
}

// ListMachines returns the org's enrolled agents.
func (s *Server) ListMachines(ctx context.Context, _ ListMachinesRequestObject) (ListMachinesResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Machines.ListMachines(ctx, p)
	if err != nil {
		return nil, mapMachineError(err)
	}
	out := make([]Machine, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toMachine(row))
	}
	return ListMachines200JSONResponse(out), nil
}

// UpdateMachine renames or revokes a machine. Both fields may be present.
func (s *Server) UpdateMachine(ctx context.Context, req UpdateMachineRequestObject) (UpdateMachineResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if req.Body.Name == nil && req.Body.Revoked == nil {
		return nil, BadRequest("name or revoked is required")
	}
	var m db.Machine
	if req.Body.Name != nil {
		name := strings.TrimSpace(*req.Body.Name)
		if name == "" {
			return nil, BadRequest("name must be at least 1 character")
		}
		m, err = s.Machines.RenameMachine(ctx, p, req.MachineId, name)
		if err != nil {
			return nil, mapMachineError(err)
		}
	}
	if req.Body.Revoked != nil {
		if !bool(*req.Body.Revoked) {
			return nil, BadRequest("revoked can only be true")
		}
		m, err = s.Machines.RevokeMachine(ctx, p, req.MachineId)
		if err != nil {
			return nil, mapMachineError(err)
		}
	}
	return UpdateMachine200JSONResponse(s.toMachine(m)), nil
}

func toEnrollToken(t db.EnrollToken) EnrollToken {
	return EnrollToken{
		Id:         t.ID,
		CreatedAt:  t.CreatedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
		LastUsedAt: t.LastUsedAt,
		UseCount:   int(t.UseCount),
	}
}

func (s *Server) toMachine(m db.Machine) Machine {
	online := false
	harnesses := []string{}
	if s.AgentHub != nil {
		online = s.AgentHub.Online(m.ID)
		harnesses = s.AgentHub.Harnesses(m.ID)
	}
	return Machine{
		Id:           m.ID,
		Name:         m.Name,
		Hostname:     m.Hostname,
		Os:           m.Os,
		Arch:         m.Arch,
		AgentVersion: m.AgentVersion,
		EnrolledAt:   m.EnrolledAt,
		LastSeenAt:   m.LastSeenAt,
		Online:       online,
		RevokedAt:    m.RevokedAt,
		Harnesses:    &harnesses,
	}
}

func mapMachineError(err error) error {
	if errors.Is(err, machine.ErrNotFound) {
		return NotFound("machine not found")
	}
	return err
}
