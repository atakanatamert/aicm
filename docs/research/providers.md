# Provider billing, usage and balance APIs: research notes

Researched 2026-10-17. Verify each endpoint against the live docs when writing its adapter, and record any difference in `docs/DECISIONS.md`.

## MVP adapters

| Provider | Endpoints | Auth | Currency | Links |
| --- | --- | --- | --- | --- |
| OpenRouter | `GET /api/v1/key` (usage daily, weekly and monthly; limit; limit_remaining). `GET /api/v1/credits` (total_credits − total_usage). `GET /api/v1/activity` (30 days by model and day). | Normal key for `/key`; management key for credits and activity (verify) | USD | Top-up https://openrouter.ai/settings/credits · [docs](https://openrouter.ai/docs/api/api-reference/credits/get-remaining-credits) |
| Anthropic API | `GET /v1/organizations/usage_report/messages`, `/usage_report/claude_code`, `GET /v1/organizations/cost_report` | Admin key `sk-ant-admin…` in `x-api-key` | USD | Billing https://platform.claude.com/settings/billing · [docs](https://platform.claude.com/docs/en/build-with-claude/usage-cost-api) |
| OpenAI API | `GET /v1/organization/usage/{completions,embeddings,images,…}`, `GET /v1/organization/costs` | Admin key `sk-admin…` as Bearer | USD | Billing https://platform.openai.com/settings/organization/billing/overview · [docs](https://platform.openai.com/docs/api-reference/usage). No balance API (OpenAI confirmed). |
| xAI | `POST /v1/billing/teams/{team_id}/usage`, `GET …/invoices`, `GET …/prepaid/balance` on `https://management-api.x.ai` | Management key | USD | https://console.x.ai · [docs](https://docs.x.ai/developers/rest-api-reference/management/billing) |
| DeepSeek | `GET https://api.deepseek.com/user/balance` returns `balance_infos[]` (total, granted and topped-up balance, plus currency) | Normal key, Bearer | CNY or USD | Top-up https://platform.deepseek.com/top_up · [docs](https://api-docs.deepseek.com/api/get-user-balance) |
| Moonshot / Kimi | `GET https://api.moonshot.ai/v1/users/me/balance` returns available, voucher and cash balance (`.cn` host for China) | Normal key, Bearer | USD or CNY | https://platform.moonshot.ai/console/pay · [docs](https://platform.kimi.ai/docs/api/balance) |
| Z.ai coding plan | See `quota-capture.md` | Normal key | USD | https://z.ai/manage-apikey/billing · https://z.ai/subscribe |
| Z.ai API (pay-as-you-go) | `GET https://api.z.ai/api/biz/account/query-customer-account-report` (undocumented console endpoint; `open.bigmodel.cn` in China) returns `availableBalance`, `rechargeAmount`, `giveAmount`, `totalSpendAmount`. Verified live 2026-10-20 with a coding-plan key: the same key reads the account balance. | Normal key, Bearer | USD (CNY in China) | https://z.ai/manage-apikey/billing |
| GitHub Copilot | See `quota-capture.md` | Fine-grained PAT with Plan: read | USD | https://github.com/settings/billing · https://github.com/settings/copilot |

## Manual with deep links

| Provider | What the user enters | Links |
| --- | --- | --- |
| Google AI Studio (Gemini API) | Tier, project spend cap, prepaid credits (then tracked by snapshot deltas). No usage, spend or balance API exists. | https://aistudio.google.com/usage · /spend · /billing · /rate-limit · /apikey · [billing docs](https://ai.google.dev/gemini-api/docs/billing) |
| Claude Pro / Max | Plan, price, renewal date (meters come from the agent) | https://claude.ai/settings/billing · https://claude.ai/settings/usage |
| ChatGPT Plus / Pro | Plan, price, renewal date (meters come from the agent) | https://chatgpt.com/#settings/Account · https://chatgpt.com/codex/settings/usage |
| Gemini / Google AI Pro, Ultra | Plan, price, renewal | https://one.google.com/settings |
| SuperGrok | Plan, price, renewal | https://grok.com (verify the subscription page) |
| Cursor (individual) | Plan, price, renewal | https://cursor.com/dashboard |
| Mistral | Balance, usage | https://console.mistral.ai/usage · https://admin.mistral.ai/organization/billing (verify) |
| Together AI | Balance | https://api.together.ai/settings/organization/~current/billing |
| Perplexity API and Pro | Balance, plan | https://console.perplexity.ai/project/billing · https://www.perplexity.ai/settings/account |
| Cohere | Usage | https://dashboard.cohere.com/billing |
| Groq | Balance (Enterprise has Prometheus metrics) | https://console.groq.com/settings/billing |
| Fireworks | Balance (CSV export via `firectl`) | https://app.fireworks.ai/account/billing |

## Later candidates (balance endpoints exist)

- **SiliconFlow:** `GET https://api.siliconflow.com/v1/user/info` returns balance, chargeBalance and totalBalance.
- **Novita:** user balance endpoint.
- **MiniMax:** `/account/query_balance` (verify).
- **Alibaba DashScope:** BSS `QueryAccountBalance`, which needs a cloud AccessKey rather than the DashScope key.
- **Cursor Teams:** Admin API with `/teams/daily-usage-data` and `/teams/spend`.

These suit declarative YAML adapters.
