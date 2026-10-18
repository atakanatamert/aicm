# 31 – Catalog: manual and agent-captured providers

**Writer:** DeepSeek Flash
**Depends on:** 04
**License of touched packages:** AGPL-3.0-or-later

## Goal

Every provider without a hub API adapter is a full catalog entry: name, billing currency, links (top-up, billing, manage, cancel, usage), model families and the meters a user or the agent fills in. API adapters (OpenRouter, Anthropic API, OpenAI API, xAI, DeepSeek, Moonshot, Z.ai, GitHub Copilot) are separate tasks and **not** part of this one.

## Files to touch

- `internal/provider/manual/manual.go` (new)
- `internal/provider/registry/registry.go` (edit: append `manual.All()`)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `internal/provider/provider.go`: `Info` (every field), `Links`, `AuthKind`, `Provider`
- `internal/meter/meter.go`: `Def`, units, reset kinds
- `internal/provider/registry/registry.go`, `internal/provider/registry/registry_test.go` (the catalog test must keep passing)
- `docs/research/providers.md` ("Manual with deep links"), `docs/research/quota-capture.md` (Claude and Codex windows)
- `docs/PLAN.md` sections "What each provider actually exposes" and "Quota model"

## Contract

```go
package manual

// entry is a catalog-only provider: it has no API the hub calls.
type entry struct{ info provider.Info }

func (e entry) Info() provider.Info { return e.info }
// Validate accepts anything: there is no credential to check.
func (e entry) Validate(context.Context, provider.Credentials) error { return nil }

// All returns every manual and agent-captured provider, in display order.
func All() []provider.Provider
```

Write the catalog as one slice literal of `provider.Info`. Each entry sets `ID`, `Name`, `Currency`, `AuthKinds`, `Links`, `Meters` (may be empty), `ModelFamilies` (may be empty), and nothing else. Exactly these entries:

| ID | Name | Currency | AuthKinds | ModelFamilies | Meters (key · label · unit · reset · window) |
| --- | --- | --- | --- | --- | --- |
| `claude` | Claude (Pro / Max) | USD | agent_capture, none | claude | `five_hour` · 5-hour limit · percent · fixed · 300; `seven_day` · Weekly limit · percent · fixed · 10080; `seven_day_opus` · Weekly limit (Opus) · percent · fixed · 10080; `seven_day_sonnet` · Weekly limit (Sonnet) · percent · fixed · 10080 |
| `chatgpt` | ChatGPT (Plus / Pro, Codex) | USD | agent_capture, none | gpt | `primary` · 5-hour limit · percent · fixed · 300; `secondary` · Weekly limit · percent · fixed · 10080; `credits` · Codex credits · credits · never |
| `google-ai-studio` | Google AI Studio (Gemini API) | USD | none | gemini | `prepaid_credits` · Prepaid credits · currency USD · never; `monthly_spend` · Spend this month · currency USD · calendar_month |
| `google-one` | Google AI Pro / Ultra | USD | none | gemini | none |
| `supergrok` | SuperGrok | USD | none | grok | none |
| `cursor` | Cursor | USD | none | claude, gpt, gemini | none |
| `mistral` | Mistral | EUR | none | mistral | `balance` · Balance · currency EUR · never |
| `together` | Together AI | USD | none | * | `balance` · Balance · currency USD · never |
| `perplexity` | Perplexity (API and Pro) | USD | none | perplexity | `balance` · API balance · currency USD · never |
| `cohere` | Cohere | USD | none | cohere | none |
| `groq` | Groq | USD | none | * | `balance` · Balance · currency USD · never |
| `fireworks` | Fireworks AI | USD | none | * | `balance` · Balance · currency USD · never |

Links: take them from `docs/research/providers.md` and `docs/research/quota-capture.md`. Map them to `TopUp`, `Billing`, `ManageSubscription`, `Cancel`, `Usage`, `Keys`, `Docs` by meaning. For subscriptions, `ManageSubscription` and `Cancel` may be the same page. Where the research says "(verify)", open the page's domain with `curl -sI` and use the URL only if it answers 200, 30x, 401 or 403; otherwise use the provider's billing page you can confirm. List any link you changed or dropped in your summary.

`registry.All` becomes:

```go
ps := []provider.Provider{ /* API adapters, one per line, added by their tasks */ }
return append(ps, manual.All()...)
```

## Done when

- [ ] `go test ./internal/provider/...` passes (the catalog test validates every entry: ids unique, currencies valid, meters valid).
- [ ] `go run ./cmd/aicm serve` on a fresh DB, log in, `GET /api/providers` lists all twelve entries if task 09 has landed; otherwise skip this check and say so.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `0a8b4aa` after the first attempt (DeepSeek Flash, ~2.5 min). Link changes by the writer, accepted: SuperGrok uses `https://grok.com/plans` (settings/subscription pages 404); Google AI Studio `/spend` and `/rate-limit` dropped (no matching `Links` field). The `/api/providers` smoke runs after task 09.
