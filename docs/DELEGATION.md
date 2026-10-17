# Delegation guide: Opus to Flash models

Opus (lead) writes contracts and task cards and verifies results. DeepSeek Flash and GLM 5.3 Flash (writers) implement one task card at a time.

## Why this works, and when it doesn't

Flash models do well when the prompt contains:
- the exact interface or SQL to satisfy
- one existing file to imitate
- a concrete done-check

They do badly when they have to invent structure. So:

- No task asks a writer to create a new package layout, a new abstraction or a new dependency.
- Every task names the files to touch. Anything outside that list is rejected.
- Keep each task under about 300 changed lines. Split anything bigger.

## Model assignment

| Area | Writer | Reason |
| --- | --- | --- |
| Go packages under `internal/`, `cmd/` | DeepSeek Flash | One model keeps Go style consistent |
| React app under `web/` | GLM 5.3 Flash | One model keeps UI style consistent |
| Contracts, scoring, release pipeline, anything that failed twice | Opus | Design work, not fill-in |

Swap the assignment if one model clearly does better in practice, and record the change in `docs/DECISIONS.md`.

## Task card

Copy `docs/tasks/TEMPLATE.md` to `docs/tasks/NN-short-name.md`. The writer gets:
- the card
- `CLAUDE.md`
- the files listed under "Read first"

Nothing else.

## Verification checklist (Opus runs this on every result)

1. The diff touches only the files listed in the card.
2. `go build ./...`, `go vet ./...` and `golangci-lint run` are clean, or for web, `pnpm build` and `pnpm lint`.
3. `sqlc generate` and the OpenAPI codegen leave no diff.
4. The tests the card requires exist and pass. No extra tests the card didn't ask for.
5. Every query filters by `org_id`.
6. No secret or token is logged, returned or sent to the hub (agent tasks).
7. SPDX headers are present and correct for the package's license.
8. Code ported from another project is attributed in `THIRD_PARTY_NOTICES.md`.
9. The structure matches neighbouring packages. No new layers, interfaces or helpers the card didn't ask for.
10. UI tasks: run Impeccable `critique` on the changed screens and fix what it flags before merging.

If a check fails, send the writer the failing check and the relevant output, not a rewrite. After the second failure, either rewrite the card with a sharper interface and example, or implement it yourself. Note the outcome at the bottom of the card.

## Parallelism

Use the dependency column in `docs/tasks/README.md`. Tasks whose dependencies are merged can run at the same time, one writer each. Merge in task-number order to keep migrations and generated code conflict-free.
