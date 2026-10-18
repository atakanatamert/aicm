# 13 – Quota: meters, readings, scoring, recommendations, forecast

**Writer:** Opus
**Depends on:** 04 (and 07, 12 for wiring)
**License of touched packages:** AGPL-3.0-or-later

## Goal

Provider-native meters and their readings are stored untouched; the Decision 11 scoring rule ranks accounts and projects capacity for 24 hours; `/api/quotas`, `/api/recommend`, `/api/forecast` and the manual meter endpoints work.

## Files

`internal/quota/{score.go,score_test.go,quota.go,price.go}`, `internal/db/queries/quota.sql`, `internal/httpapi/{quota.go,summary.go}`, `internal/provider/provider.go` (`Info.ModelFamilies`), wiring in `cmd/aicm/main.go` and `httpapi.Deps.Quota`.

## Done when

- [x] Scoring cases agree with the written rule (`score_test.go`: expiring window, runs dry before reset, exhausted, stale reading, wallet with a top-up, window change, single reading, no data, weakest meter, rank order, forecast with reset and with a wallet running dry).
- [x] Smoke against a live hub: quota cards show provider figures plus scores; recommend returns reasons ("30% of the 5-hour limit expires in 3 h unused"); forecast returns hourly points; manual meter and reading endpoints validate (percent > 100 rejected, manual key over a reported meter → 409); unauthenticated → 401.

## Outcome (filled in by Opus)

Done by Opus (task kept per the plan), merged in `de3afd1`. Decisions:
- Burn rate = time-weighted mean over consecutive reading pairs in a 6 h lookback (7 days for wallets and monthly meters); pairs that cross a reset (different `resets_at`) or go backwards (top-ups) are skipped.
- A stale reading (past `resets_at`) counts as a full window; the next reset is assumed one window later.
- Expiring shares within half a percentage point tie, so reading noise doesn't reorder accounts.
- Job size: medium needs 10% of every limit left after the horizon, large 30%.
- Marginal cost: 3:1 input:output blend of the family's representative model from the LiteLLM price table (`quota.FamilyModels`); flat-rate subscriptions cost 0; unknown prices rank last.
