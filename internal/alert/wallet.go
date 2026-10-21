// SPDX-License-Identifier: AGPL-3.0-or-later

package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/db"
)

// evalWallets warns when a harness on a subscription account bills an API
// key, and once when the subscription has paid extra usage turned on.
func (s *Service) evalWallets(ctx context.Context, now time.Time, rules ruleSet,
	accounts []db.Account, emit func(candidate) error) error {
	rows, err := s.q.ListAlertWallets(ctx)
	if err != nil {
		return fmt.Errorf("listing harness wallets: %w", err)
	}
	byID := make(map[string]db.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}
	for _, row := range rows {
		a, ok := byID[row.AccountID]
		if !ok {
			continue
		}
		rule, ok := rules.get(a.OrgID, a.ID, "wrong_wallet")
		if !ok || !ruleActive(rule, now) {
			continue
		}
		var details map[string]string
		if len(row.Details) > 0 && string(row.Details) != "null" {
			_ = json.Unmarshal(row.Details, &details)
		}
		name := displayName(a, s.infoFor(a.ProviderID))
		if details[agentlink.DetailBillingMode] == "api_key" {
			if err := emit(candidate{
				orgID:     a.OrgID,
				accountID: &a.ID,
				machineID: &row.MachineID,
				kind:      "wrong_wallet",
				eventKey:  fmt.Sprintf("wallet:%s:%s", a.ID, row.MachineID),
				severity:  "warning",
				title:     fmt.Sprintf("%s on %s bills an API key, not %s", row.Harness, row.MachineName, name),
				body:      "The harness is set up to bill a plain API key while the subscription is active. Point it at the subscription or remove the key.",
			}); err != nil {
				return err
			}
		}
		if details[agentlink.DetailExtraUsage] == "on" {
			if err := emit(candidate{
				orgID:     a.OrgID,
				accountID: &a.ID,
				kind:      "wrong_wallet",
				eventKey:  fmt.Sprintf("extra_usage:%s", a.ID),
				severity:  "info",
				title:     fmt.Sprintf("Paid extra usage is on for %s: past its limits it charges your card", name),
				body:      "Turn extra usage off if reaching the plan's limits should stop the work instead.",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
