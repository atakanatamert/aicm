// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// errUnknownProvider marks a ref whose provider is not in the catalog.
var errUnknownProvider = errors.New("unknown provider")

// ingestAccounts stores a full snapshot: one account per external id and one
// account_machines row per (account, machine, harness).
func (h *Hub) ingestAccounts(ctx context.Context, s *session, e agentlink.Envelope) string {
	snapshot, err := agentlink.Decode[agentlink.Accounts](e)
	if err != nil {
		return err.Error()
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting account ingest: %w", err).Error()
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := h.q.WithTx(tx)

	skipped := 0
	actor := auth.Principal{OrgID: s.orgID, Kind: auth.ActorSystem, Label: s.machineName}.Actor()
	for _, item := range snapshot.Accounts {
		if item.Ref.ExternalID == "" {
			skipped++
			continue
		}
		if _, ok := h.providerInfo(item.Ref.ProviderID); !ok {
			skipped++
			continue
		}

		a, err := h.resolveAccount(ctx, q, s.orgID, item)
		if err != nil {
			return fmt.Errorf("resolving account %s/%s: %w", item.Ref.ProviderID, item.Ref.ExternalID, err).Error()
		}
		details, err := discoveredDetails(item)
		if err != nil {
			return err.Error()
		}
		if err := q.UpsertAccountMachine(ctx, db.UpsertAccountMachineParams{
			OrgID:     s.orgID,
			AccountID: a.ID,
			MachineID: s.machineID,
			Harness:   item.Harness,
			Details:   details,
		}); err != nil {
			return fmt.Errorf("recording account machine: %w", err).Error()
		}
		if err := h.fillDetectedPlan(ctx, q, actor, s.orgID, a, item); err != nil {
			return err.Error()
		}
		if a.Inserted {
			if err := audit.Write(ctx, q, actor, audit.Entry{
				Action:     "account.discover",
				TargetKind: "account",
				TargetID:   a.ID,
				After: map[string]any{
					"provider_id": a.ProviderID,
					"external_id": item.Ref.ExternalID,
					"status":      a.Status,
					"harness":     item.Harness,
					"machine":     s.machineName,
				},
			}); err != nil {
				return err.Error()
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing account ingest: %w", err).Error()
	}
	if skipped > 0 {
		return fmt.Sprintf("%d accounts skipped: unknown provider or missing external id", skipped)
	}
	return ""
}

// ingestUsage stores a batch of usage events, dropping dismissed accounts.
func (h *Hub) ingestUsage(ctx context.Context, s *session, e agentlink.Envelope) string {
	batch, err := agentlink.Decode[agentlink.Usage](e)
	if err != nil {
		return err.Error()
	}
	if len(batch.Events) > agentlink.MaxBatchItems {
		return fmt.Sprintf("usage batch has %d events, limit %d", len(batch.Events), agentlink.MaxBatchItems)
	}

	accountIDs := make(map[agentlink.AccountRef]string, len(batch.Events))
	events := make([]agentlink.UsageEvent, 0, len(batch.Events))
	for _, ev := range batch.Events {
		a, err := h.resolveAccount(ctx, h.q, s.orgID, agentlink.DiscoveredAccount{Ref: ev.Account})
		if err != nil {
			return err.Error()
		}
		if a.Status == "dismissed" {
			continue
		}
		accountIDs[ev.Account] = a.ID
		events = append(events, ev)
	}
	if len(events) == 0 {
		return ""
	}
	if _, err := h.usage.RecordAgentEvents(ctx, s.orgID, s.machineID, accountIDs, events); err != nil {
		return fmt.Errorf("storing usage: %w", err).Error()
	}
	return ""
}

// ingestMeters stores a batch of meter readings, dropping dismissed accounts.
func (h *Hub) ingestMeters(ctx context.Context, s *session, e agentlink.Envelope) string {
	batch, err := agentlink.Decode[agentlink.Meters](e)
	if err != nil {
		return err.Error()
	}
	if len(batch.Reports) > agentlink.MaxBatchItems {
		return fmt.Sprintf("meters batch has %d reports, limit %d", len(batch.Reports), agentlink.MaxBatchItems)
	}

	machineID := s.machineID
	for _, report := range batch.Reports {
		a, err := h.resolveAccount(ctx, h.q, s.orgID, agentlink.DiscoveredAccount{Ref: report.Account})
		if err != nil {
			return err.Error()
		}
		if a.Status == "dismissed" {
			continue
		}
		if _, err := h.quota.RecordReadings(ctx, s.orgID, a.ID, report.Def,
			[]meter.Reading{report.Reading}, &machineID); err != nil {
			return fmt.Errorf("storing meter reading: %w", err).Error()
		}
	}
	return ""
}

// resolveAccount finds or creates the account for a ref. The upsert makes
// concurrent reports from several machines converge on one row.
func (h *Hub) resolveAccount(ctx context.Context, q *db.Queries, orgID string, item agentlink.DiscoveredAccount) (db.UpsertDiscoveredAccountRow, error) {
	info, ok := h.providerInfo(item.Ref.ProviderID)
	if !ok {
		return db.UpsertDiscoveredAccountRow{}, fmt.Errorf("%w %q", errUnknownProvider, item.Ref.ProviderID)
	}
	if item.Ref.ExternalID == "" {
		return db.UpsertDiscoveredAccountRow{}, errors.New("account external_id is required")
	}

	name := strings.TrimSpace(item.Label)
	if name == "" {
		name = info.Name
	}
	// Agents label keys "<provider id> key …1234"; show the catalog name.
	if rest, ok := strings.CutPrefix(name, item.Ref.ProviderID+" key "); ok {
		name = info.Name + " " + rest
	}
	kind := item.AuthKind
	if kind == "" {
		kind = string(provider.AuthAgentCapture)
	}
	externalID := item.Ref.ExternalID

	a, err := q.UpsertDiscoveredAccount(ctx, db.UpsertDiscoveredAccountParams{
		OrgID:      orgID,
		ProviderID: item.Ref.ProviderID,
		Name:       name,
		ExternalID: &externalID,
		AuthKind:   kind,
		Currency:   info.Currency,
		PlanName:   strings.TrimSpace(item.Plan),
		// Providers whose only data source is undocumented work out of the
		// box; the account page can turn it off (Decision 9, revised).
		AllowUnofficial: info.Unofficial,
	})
	if err != nil {
		return db.UpsertDiscoveredAccountRow{}, fmt.Errorf("upserting account: %w", err)
	}
	return a, nil
}

// discoveredDetails encodes the non-secret extras stored on account_machines.
func discoveredDetails(item agentlink.DiscoveredAccount) (json.RawMessage, error) {
	details := make(map[string]string, len(item.Details)+3)
	for k, v := range item.Details {
		details[k] = v
	}
	details["has_api_key"] = strconv.FormatBool(item.HasAPIKey)
	if item.AuthKind != "" {
		details["auth_kind"] = item.AuthKind
	}
	if item.Plan != "" {
		details["plan"] = item.Plan
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("encoding account details: %w", err)
	}
	return raw, nil
}

// providerInfo finds a compiled-in catalog entry.
func (h *Hub) providerInfo(id string) (provider.Info, bool) {
	for _, p := range h.providers {
		if p.Info().ID == id {
			return p.Info(), true
		}
	}
	return provider.Info{}, false
}

// fillDetectedPlan prices an active subscription the agent identified (plan
// tier and start date) when the user has not entered a plan yet.
func (h *Hub) fillDetectedPlan(ctx context.Context, q *db.Queries, actor audit.Actor, orgID string, a db.UpsertDiscoveredAccountRow, item agentlink.DiscoveredAccount) error {
	if a.Status != "active" {
		return nil
	}
	tier := item.Details[agentlink.DetailPlanTier]
	startedRaw := item.Details[agentlink.DetailSubscriptionStarted]
	if tier == "" || startedRaw == "" {
		return nil
	}
	info, ok := h.providerInfo(item.Ref.ProviderID)
	if !ok {
		return nil
	}
	price, ok := info.Plan(tier)
	if !ok {
		return nil
	}
	started, err := time.Parse(time.RFC3339Nano, startedRaw)
	if err != nil {
		return nil // a malformed date from the harness: leave the plan for the user
	}
	if _, err := account.FillDetected(ctx, q, actor, orgID, a.ID, account.DetectedPlan{Price: price, Started: started}, time.Now()); err != nil {
		return fmt.Errorf("filling detected plan for %s: %w", a.ID, err)
	}
	return nil
}
