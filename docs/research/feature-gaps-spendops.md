# aicm feature gaps: spend ops, budgets, self-hosting (GapsSpendOps)

Research date 2026-10-21. Scope: API wallets and top-ups, invoices/VAT/receipts, per-project/per-client allocation, budgets and hard caps, anomaly detection, renewals and cancellations, shared seats, FinOps for LLMs, r/selfhosted expectations (SSO/OIDC, backups, Docker, mobile/PWA, ntfy/Telegram/Slack/Discord/webhooks, Home Assistant, API), and data locality.

**Method notes**
- Reddit blocked direct fetches (HTTP 403 and a "prove your humanity" page), so Reddit quotes are **search-result excerpts**. They are marked *(excerpt)*. Everything else was read at the source.
- Before ranking, I checked the repo so nothing below repeats what aicm already has or plans:
  - `docs/PLAN.md:301`: "Budgets and forecasts of spend | Needs months of history first | After MVP".
  - `docs/PLAN.md:293`: "Slack, Telegram, ntfy alerts | Email first, through the `Notifier` interface | Next".
  - `docs/PLAN.md:295`: "SSO / OIDC | Not needed for a self-hosted tool | Not planned". Line 52 puts "SSO/SCIM" in a later commercial `ee/`. Line 49 lists "push, Slack and Telegram channels" in the hosted column.
  - `docs/tasks/14a-alerts.md:74-80`: the alert kinds are renewal, promo_ending, low_balance, meter_exhausted, quota_waste, sync_failing and agent_offline. There is no budget, anomaly or credit-expiry alert.
  - `api/openapi.yaml:925,951`: spend groups by `provider|account` and usage by `day|model|account|provider`. There is no project, tag or cost-center dimension.
  - `api/openapi.yaml:1967-1985`: `Charge` has no tax split, invoice number, attachment or expiry. `Account` has `notes`, `renews_on`, `auto_renew` and `promo_ends_on`, but no tags or seats.
  - `docs/tasks/23-claude-usage-poll.md:39`: "`extra_usage` (when `is_enabled`) → no meter in this task".
  - Already present: nightly `pg_dump` + `master.key` backups (`compose.yaml:36-53`, `README.md:28`) and API tokens. There is no PWA manifest or i18n in `web/`, and no TOTP/2FA or forward-auth anywhere.
- Frequency scale: **High** = 4 or more independent sources or communities; **Medium** = 2–3; **Low** = 1 or niche.
- Size, for aicm: **S** ≈ days, **M** ≈ 1–2 weeks, **L** = multi-week or needs a new subsystem. All sizes are [INFERENCE].

---

## Ranked missing features

### 1. Budgets with threshold alerts and a month-end projection
**What users ask for:** a dollar budget per org, provider, account, group or project. They want alerts at 50/80/100%, plus "projected month-end vs budget". They want this *before* the bill or the provider's cap hits.

**Evidence**
- OpenRouter documents that it has caps but no warnings: "Does OpenRouter alert me before I hit a budget? **Not yet.** When you hit a cap, requests get a 403, and there's no email or webhook warning beforehand." It repeats "No email or webhook alerts yet" for workspace budgets. ([OpenRouter blog, 2026-08/09](https://openrouter.ai/blog/insights/governing-team-ai-spend/))
- OpenAI users: "It is only possible to have notifications, to set a budget, but not a hard limit… I'm considering leaving OpenAI for this unique reason." ([OpenAI forum, 2026-01](https://community.openai.com/t/should-all-users-have-hard-spending-caps/1370683)) Another user: "I set it to $10 BECAUSE I wanted a hard limit at $10… $82!!!" (same thread). See also "[I'd like OpenAI to allow setting a hard limit](https://community.openai.com/t/i-d-like-openai-to-allow-setting-a-hard-limit-as-well-since-the-openai-api-uses-prepaid-billing/1370125)", "[Project limits not being respected](https://community.openai.com/t/project-limits-not-being-respected/1300634)" ("project limits are no longer hard limits, and are notification only") and "[API-key specific … spending limits would be good](https://community.openai.com/t/api-key-specific-rate-and-spending-limits-would-be-good/1374358)".
- Cursor rebuilt "spend alert" in 2026: "configure alerts based on dollar thresholds — per member or team-wide — delivered via Slack or email before an unexpected charge lands". The New Stack framed the problem as "the AI token bill is growing, and nobody has the tools to manage it yet." ([The New Stack, 2026-06-04](https://thenewstack.io/cursor-pricing-token-billing/))
- A Home Assistant integration built for this case exposes a "projected month-end cost" and an example automation for "Projected month-end cost exceeds configured budget". ([HAAnthropicUsageMonitor](https://github.com/burren2025/HAAnthropicUsageMonitor))
- r/claude *(excerpt)*: "Since everything went usage-based, I genuinely have no idea which sessions are burning money. I've seen people mention getting blindside…" ([r/claude](https://www.reddit.com/r/claude/comments/1vftn8s/anyone_else_lost_track_of_what_theyre_actually/)). r/OpenAI *(excerpt)*: "at our company the AI bill has started getting kind of ridiculous" ([r/OpenAI](https://www.reddit.com/r/OpenAI/comments/1sh5f8z/how_much_money_are_you_guys_spending_on_ai_tools)). r/cursor: "Out of control Cursor Usage & Billing? $500+ bills?" ([r/cursor](https://www.reddit.com/r/cursor/comments/1umhgjy/out_of_control_cursor_usage_billing_500_bills)).
- FinOps Foundation lists Budgeting and Forecasting as core for AI and warns that forecasting is harder "leading to more forecast variance, requiring shorter forecasting windows". ([FinOps for AI](https://www.finops.org/framework/technology-categories/ai/))

**How often:** High. It appears in the OpenAI forum, the OpenRouter docs, Cursor, HA integrations and several subreddits.

**Competitors that have it:** LiteLLM ([soft budgets, per key/user/team/tag](https://docs.litellm.ai/docs/proxy/alerting), [tag budgets](https://github.com/BerriAI/litellm-docs/blob/main/docs/proxy/tag_budgets.md)), [Portkey budget policies](https://portkey.ai/docs/product/enterprise-offering/budget-policies), [Helicone cost alerts](https://docs.helicone.ai/features/alerts), [GitHub budgets for metered products](https://docs.github.com/en/billing/concepts/budgets-and-alerts), Cursor and Wallos (budget in stats; [wallos-mcp](https://github.com/mkpoli/wallos-mcp) reads "budgets").

**Size:** M. aicm already has money out, per-account spend, burn rate and the Notifier, so this needs a `budgets` table, an evaluator and UI. The repo defers it "until months of history". A month-to-date budget alert needs no history; only the projection gets better with more data [INFERENCE].

### 2. Spend-anomaly and runaway detection
**What users ask for:** an alert when spend, top-up frequency or token burn breaks from the account's own baseline, so a leaked key, a looping agent or a billing bug is caught in hours rather than at invoice time.

**Evidence**
- A Gemini key was stolen and ran up "$82,314.44" in 48 h. The company "usually spends $180 a month" ([The Register, 2026-03](https://www.theregister.com/security/2026/03/03/dev-stunned-by-82k-gemini-api-key-bill-after-theft/4925029)). Also: "~₹8.3 lakh … in less than 2 hours" ([Google AI forum](https://discuss.ai.google.dev/t/google-ai-studio-api-key-compromised-8-3-lakh-in-gemini-3-pro-image-charges-in-less-than-2-hours-days-after-a-suspension-and-reinstatement-support-is-investigating-looking-for-advice-from-anyone-who-has-been-through-this/143565)) and "from ~$7/month to a six-figure bill in 8 days" ([Google AI forum](https://discuss.ai.google.dev/t/not-a-leaked-api-key-13-foreign-projects-were-attached-to-our-dormant-billing-account-from-7-month-to-a-six-figure-bill-in-8-days/188802/1)).
- An OpenAI user caught key abuse by an ex-employee through the top-up pattern: "the credits of my account had been funded twice in less than 24h (normally, it is +/- once per month)". 108 M tokens were used before they stopped it. ([OpenAI forum](https://community.openai.com/t/should-all-users-have-hard-spending-caps/1370683)) This is exactly the signal aicm's suggested top-up charges already see.
- Claude Code: "~$6,000" overnight; "a parallel-subagent run silently auto-purchased $864"; "a stray ANTHROPIC_API_KEY … surprise $187 charge" ([runaway-cost checklist gist, 2026-06](https://gist.github.com/yurukusa/a0b66592fe016d3823b8090d25af1a18)).
- More OpenAI billing-problem threads: "$1,893 Fraudulent Balance after Hacker bypassed Hard Limit", "$563.88 debt accrued on a $5 Prepaid Account", "charged 3 times for the same month… (total of 20k usd) that don't match… my usage (11k usd)" ([tag index](https://community.openai.com/tag/api-billing-problem/552)). One HN user wrote "My anthropic key was leaked and someone ran up a 10k bill" ([HN, 2024](https://news.ycombinator.com/item?id=40546058)).
- FinOps defines an "Anomaly Detection Rate" KPI: "Total Cost of Anomaly Spikes / Total AI Spend". It notes that "the likelihood of anomalous spending will increase as teams experiment". ([FinOps for AI](https://www.finops.org/framework/technology-categories/ai/))
- The HA Anthropic integration ships automations for "Today's spend exceeds a threshold" and "A new unknown API key ID appears". ([HAAnthropicUsageMonitor](https://github.com/burren2025/HAAnthropicUsageMonitor))

**How often:** High, with the highest dollar stakes of anything in this report.

**Competitors that have it:** Google Cloud Cost Anomaly Detection ([blog](https://cloud.google.com/blog/topics/cost-management/cloud-cost-management-enhancements-at-finops-x-2024)), Finout, CostLynx and other paid FinOps SaaS. No self-hosted subscription tracker I found does this.

**Size:** M. Proposed rules:
- daily spend > k× the trailing median;
- top-ups per 24 h > baseline;
- burn rate means a wallet will be empty in < X h (part of this already exists as `HoursToEmpty`);
- a new API key or model appears in a cost report;
- spend on a supposedly idle or archived account.

### 3. Notification channels beyond SMTP: webhooks, ntfy, Gotify, Telegram, Discord, Slack, Pushover
**What users ask for:** push to the phone and team-chat channels. A generic outbound webhook is the minimum.

**Evidence**
- Wallos, the closest self-hosted analog: "Notifications: Wallos supports multiple notification methods (email, discord, pushover, telegram, gotify and webhooks)" ([README](https://github.com/ellite/Wallos)). These were added in response to requests such as "[FEATURE REQUEST] Telegram bot notifications" ([#257](https://github.com/ellite/Wallos/issues/257)), and an ntfy method came later ([commit "feat: add ntfy as notification method (#377)"](https://git.joshuacoles.me/joshuacoles/wallos/commit/65edf0963b73deff0f0f7f04427e69ce335bd776)). Users then ask for help wiring Wallos to their own ntfy ([Cloudron forum](https://forum.cloudron.io/topic/13355/notifications-not-working)).
- r/selfhosted *(excerpt)*: "Ntfy is awesome. It's one of the default notification options with uptime kuma and I use it for outage alerts." ([thread](https://www.reddit.com/r/selfhosted/comments/1j5yuxh/what_notification_software_do_you_guys_use))
- LiteLLM budget alerts work across Slack, Discord and MS Teams, plus "`ALERTING_WEBHOOK_URL` … any Slack-compatible incoming webhook, such as Rocket.Chat or Mattermost" ([docs](https://docs.litellm.ai/docs/proxy/alerting)). Cursor and Helicone alert via Slack ([New Stack](https://thenewstack.io/cursor-pricing-token-billing/), [Helicone](https://docs.helicone.ai/guides/cookbooks/cost-tracking)).
- Several Claude/Codex usage notifiers and HA integrations exist only to push limit and credit events (see #13).

**How often:** High among self-hosters. It is baseline parity with Wallos.

**Competitors that have it:** Wallos, LiteLLM, Cursor, Helicone and Uptime Kuma (the ntfy reference).

**Size:** S for generic JSON webhooks plus ntfy/Gotify, which are simple HTTP POSTs behind the existing `Notifier`. S–M for Telegram, Discord and Slack.

**Positioning risk:** `docs/PLAN.md:49` lists "push, Slack and Telegram channels" as a hosted advantage. Self-hosters read channel paywalls the same way they read the PLANKA SSO move (#8) [INFERENCE]. Keeping webhooks and ntfy in the AGPL core avoids that reading.

### 4. Credit-lot expiry tracking and "credits expiring" alerts
**What users ask for:** each top-up or grant carries an expiry date. The UI should show "$X expires on D" next to the balance and remind before it lapses.

**Evidence**
- "I feel scammed: paid $1,600 in OpenAI API credits, just $0.28 used, then gone." The user's wish list: "Show upcoming expirations clearly alongside the credit balance. Send advance reminders before paid credits expire." ([OpenAI forum, 2026-10](https://community.openai.com/t/i-feel-scammed-paid-1-600-in-openai-api-credits-just-0-28-used-then-gone-the-expiry-was-never-communicated-to-me-at-any-point/1403086)) Related threads: [Credit Expired Without Notification](https://community.openai.com/t/credit-expired-without-notification/1256598) and [Paid credits expired wth?](https://community.openai.com/t/paid-credits-expired-wth/1041718).
- In another case, expiry cascaded into a surprise charge: "Last year's credits expired & the client's card was auto-charged $106.40 today." ([OpenAI forum](https://community.openai.com/t/unauthorized-api-billing-charge-tried-contacting-support-but-it-doesnt-work-please-help/1360837))
- Expiry is now the norm across providers:
  - OpenAI: "Purchased credits expire after 1 year" ([help](https://help.openai.com/en/articles/8264644-setting-up-and-managing-prepaid-api-billing)).
  - Anthropic: "Usage Credits expire one calendar year…" ([credit terms](https://www.anthropic.com/legal/credit-terms)).
  - Gemini Prepay: "unused credits expire after 12 months" ([billing](https://ai.google.dev/gemini-api/docs/billing)).
  - Claude Max/Team API credits ($100/$200/up to $500 a month): "Unused credits expire at the end of each billing cycle… don't roll over" ([docs](https://platform.claude.com/docs/en/about-claude/api-credits-for-subscribers)).
  - Claude usage credits in Japan expire after six months ([help](https://support.claude.com/en/articles/12429409-manage-usage-credits-for-paid-claude-plans)).
- Competitor signal: CodexBar added "Claude Platform … Console API credit balance, **expiring credit grants**" and "TypeSafe … **credit expirations**" ([README](https://github.com/steipete/CodexBar)).

**How often:** High. The cost per incident is large and the trigger is fully predictable.

**Competitors that have it:** CodexBar (display only). No self-hosted tracker found.

**Size:** S–M.
- Add `expires_on` to top-up charges, plus FIFO burn-down to estimate the expiring remainder.
- Alert at 30/7/1 days.
- The Claude Max monthly API credit is a natural fit for the existing quota_waste logic.
- `promo_ends_on` already exists, but it is per account, not per credit lot.

### 5. Cost allocation: project, repo, client and cost-center tags (chargeback/showback)
**What users ask for:** group spend and usage by project, repo or client. Agencies and freelancers want a per-client export to re-bill, sometimes with a markup.

**Evidence**
- Cursor forum: "We are a consulting firm… We need to track AI usage per repository to accurately attribute costs to each client." A later reply: "I'm amazed this isnt possible… Local db extraction seems like a wild non-enterprise workaround." ([Cursor forum, 2026](https://forum.cursor.com/t/per-repository-usage-tracking-for-cost-attribution/154687))
- AWS: "the bill keeps climbing—but you have no idea who's responsible for what… Which cost center should be charged for which portion of the Bedrock bill?" ([AWS Builder Center, 2026-01](https://builder.aws.com/content/2iQjuDjUHNYpgvRtFF5G9LpcmoC/tracking-claude-code-usage-on-amazon-bedrock-by-user-or-team)). AWS added Bedrock attributes to Cost Explorer and Budgets in Oct 2026 ([AWS](https://aws.amazon.com/about-aws/whats-new/2026/10/aws-bedrock-attributes-in-cost-explorer/)).
- Cursor's enterprise org dashboard was built "giving finance teams the visibility to run chargebacks by business unit" ([New Stack](https://thenewstack.io/cursor-pricing-token-billing/)).
- Several vendors exist for this use case: "Track Claude Code spend by client and project, not just by month" ([Keito](https://keito.ai/solutions/ai-agent-cost-tracking/claude-code-cost-tracking/)). Inbox Ledger routes invoices "tagged with employee name, allocated to the AI Tools budget" ([Inbox Ledger](https://www.inboxledger.app/blog/how-to-download-chatgpt-plus-invoices)).
- FinOps: "fast moving teams may not be tagging or identifying their spend in all cases" ([FinOps for AI](https://www.finops.org/framework/technology-categories/ai/)).

**How often:** High for teams and agencies; Medium for solo users.

**Competitors that have it:**
- ccusage `--instances`, "Group Claude Code by project/instance" ([README](https://github.com/ccusage/ccusage/blob/main/apps/ccusage/README.md)).
- codeburn: "one project per git repository… spend by branch", "spend per pull request" ([docs](https://codeburn.app/docs)).
- [Helicone custom properties](https://docs.helicone.ai/features/advanced-usage/custom-properties) and LiteLLM tag budgets.
- OpenAI Costs API `group_by: project_id|user_id|api_key_id` ([ref](https://developers.openai.com/api/reference/resources/admin/subresources/organization/subresources/usage/methods/costs/index.md)); Anthropic cost report by workspace.
- OpenRouter export by API key or creator.

**Size:** M.
- The agent already reads per-project logs: Claude Code `~/.claude/projects/<dir>`, Codex rollout cwd [INFERENCE]. Keep a `project` key on usage events, add tag rules (path glob → client), and add a `group_by=project|tag` option to spend and usage.
- Provider-side projects and workspaces (OpenAI and Anthropic cost APIs) can feed the same tag dimension.

### 6. Spend on subscription overages and "subscription but metered" leaks
**What users ask for:**
- Track the money that flows *around* a flat subscription: Claude usage/extra credits, Codex credits, Cursor on-demand, and auto-reload purchases.
- Warn when an agent bills an API key while a subscription with headroom is available.

**Evidence**
- Claude usage credits switch Pro/Max to "pay-as-you-go pricing… until you reach your monthly spend limit, or your balance runs out", with auto-reload ([help](https://support.claude.com/en/articles/12429409-manage-usage-credits-for-paid-claude-plans)). The HA integration "Claude Usage" (58★, 20 forks) exposes "Extra Usage Credits – Credits consumed this month" and "Extra Usage Limit" ([repo](https://github.com/trickv/hass-claude-usage)). A Claude Code feature request asks for "Extra usage credit balance — remaining extra usage credits" ([#44328](https://github.com/anthropics/claude-code/issues/44328)).
- On metered "leaks": "If `ANTHROPIC_API_KEY` is set, Claude Code bills pay-as-you-go API — silently — even when your subscription has room". Also, "As of 2026-06-15, interactive and programmatic usage bill to separate pools" ([gist](https://gist.github.com/yurukusa/a0b66592fe016d3823b8090d25af1a18)). Codex: "I logged in using 'Log in with ChatGPT'… But Codex CLI found and used OPENAI_API_KEY from my project's .env file" ([OpenAI forum](https://community.openai.com/t/unexpected-api-charges-from-codex-cli-despite-chatgpt-login/1358277)).
- Codex credits: "I bought $10 of Codex credits… OpenAI charged my card 14 additional times" *(excerpt)* ([r/codex](https://www.reddit.com/r/codex/comments/1uu9dwu/warning_buying_codex_credits_through_the_desktop)). "Codex Auto-Reload charged $100 immediately" ([OpenAI forum](https://community.openai.com/t/codex-auto-reload-charged-100-immediately-without-disclosing-an-immediate-purchase/1391069)).
- Gemini added a "Monthly auto-charge limit… helps prevent unexpected costs from frequent automatic credit reloads" ([billing](https://ai.google.dev/gemini-api/docs/billing)). A refund request for an "Unintended auto-recharge of Gemini API credits" is on the [Google AI forum](https://discuss.ai.google.dev/t/refund-request-unintended-auto-recharge-of-gemini-api-credits-5-000-jpy/183522).

**How often:** High in 2026. Every major subscription now has a metered overflow.

**Competitors that have it:** hass-claude-usage (extra usage) and CodexBar (credits). No tool I found ties overage spend into a money-out ledger.

**Size:** S–M.
- Map `extra_usage` (already returned by the poll aicm makes) to a currency meter and to charges.
- Add a "metered-while-subscribed" alert: the agent sees API-key-authenticated usage on a machine that also has an active subscription for the same harness [INFERENCE: needs a harness auth-mode signal].
- This overlaps with the dev-tools research; the money side belongs here.

### 7. Provider-side caps: show them, sync them, optional kill switch
**What users ask for:** see each provider's own cap (org, project, workspace or key limit) next to aicm's view. Optionally set caps or disable a key from one place when an anomaly fires.

**Evidence**
- OpenAI now has enforced caps ("To stop traffic after tracked spend reaches a configured amount, enforce a hard spend limit for your organization or an individual project") and an Admin API for them ([guide](https://developers.openai.com/api/docs/guides/spend-limits), [API](https://developers.openai.com/api/reference/resources/admin/subresources/organization/subresources/projects/subresources/spend_limit)). Its key-safety guidance says: "enable hard enforcement to help limit unexpected costs from a leaked key" ([help](https://help.openai.com/en/articles/5112595-best-practices-for-api-key-safety)).
- Anthropic exposes a Spend Limits API: org and workspace, daily/weekly/monthly, with a `spend-limit-reads` beta. Setting limits is early access ([API](https://platform.claude.com/docs/en/api/beta/organization/spend_limits)). Its docs say: "To cap spending on a project, give it its own workspace and set that workspace's spend limit" ([docs](https://platform.claude.com/docs/en/about-claude/api-credits-for-subscribers)).
- OpenRouter per-key `limit` + `limit_reset` (daily/weekly/monthly) on every plan, and a Management API that can "automatically disable keys that exceed limits" ([docs](https://openrouter.ai/docs/guides/overview/auth/management-api-keys), [blog](https://openrouter.ai/blog/insights/governing-team-ai-spend/)).
- Gemini has project spend caps (UI only) ([billing](https://ai.google.dev/gemini-api/docs/billing), [Google blog](https://blog.google/innovation-and-ai/technology/developers-tools/more-control-over-gemini-api-costs/)).
- Caps fail in practice: "Hacker bypassed Hard Limit", and "Long-running tasks… may incur overages beyond your project spend cap" ([OpenAI tag](https://community.openai.com/tag/api-billing-problem/552), [Gemini billing](https://ai.google.dev/gemini-api/docs/billing)). Seeing the cap next to actual spend is therefore useful.

**How often:** Medium–High. It is usually raised together with #1 and #2.

**Competitors that have it:** CodexBar shows "spending-limit tracking" for DeepInfra and Munito, and AWS Bedrock monthly budgets ([README](https://github.com/steipete/CodexBar)). Gateways (LiteLLM, Portkey, OpenRouter) enforce caps natively.

**Size:**
- S: read-only display of provider caps as meters (OpenAI, Anthropic reads, OpenRouter key limits).
- M–L: write or kill-switch actions, which need careful roles and audit, and conflict with "MCP no tool deletes data" style guardrails [INFERENCE].

### 8. SSO/OIDC login, plus TOTP 2FA and trusted forward-auth
**What users ask for:** log in with Authentik, Authelia, Keycloak, Pocket ID or Zitadel. Failing that, honor a reverse-proxy auth header. Add TOTP for local accounts.

**Evidence**
- solidtime OIDC request: 81 votes. "Many persons in the selfhost/homelab community already use ID Providers like Keycloak, Authelia and Authentik… want therefore to integrate additional applications with OIDC"; "Another Authentik user here, and OIDC is essential!"; "started to use Pocket ID everywhere… rely on passkeys" ([discussion](https://github.com/solidtime-io/solidtime/discussions/238)).
- PLANKA moved OIDC to Pro in Aug 2026 and got: "Time to fork"; "SSO or 2FA or auto-logout are… basic features and should not be put behind a paywall"; "SSO is not an enterprise-only feature. Many of us run homelabs…" with a link to [sso.tax](https://sso.tax/) ([issue #1754](https://github.com/plankanban/planka/issues/1754); [r/selfhosted](https://www.reddit.com/r/selfhosted/comments/1vinnjt/planka_removing_sso_from_community_edition)). At the same time PLANKA moved TOTP 2FA *into* Community.
- Wallos ships OIDC with env-var config (`OIDC_ISSUER` discovery, `OIDC_DISABLE_PASSWORD_LOGIN`, `OIDC_AUTO_CREATE_USER`) ([README](https://github.com/ellite/Wallos)). Invoice Ninja has an "OIDC support for selfhosted" request ([#10839](https://github.com/invoiceninja/invoiceninja/issues/10839)).
- aicm-specific stakes: the hub stores admin and billing keys that can read cost reports and, with #7, change caps. Password-only login is a weak gate for that [INFERENCE].

**How often:** High on r/selfhosted.

**Competitors that have it:** Wallos (OIDC), PLANKA (2FA in Community) and most popular self-hosted apps.

**Size:**
- M: generic OIDC (discovery, auto-create, map groups to roles).
- S: TOTP.
- S: trusted `Remote-User` header behind an allowlisted proxy.

**Conflict with plan:** `docs/PLAN.md:295` ("Not planned") and `:52` (SSO later in `ee/`). Main should weigh this against the PLANKA backlash.

### 9. Invoices, receipts and tax (VAT/GST/JCT)
**What users ask for:**
- Attach or keep invoice PDFs on charges, and split net, tax and gross.
- Store a VAT ID and reverse-charge flag per account.
- Flag "missing invoice for month X", and export an accountant-ready pack.

**Evidence**
- On collecting invoices in teams: "send them all a Slack reminder to download their monthly invoice, wait three days, get eight invoices back, chase the other twelve." Also: "individual employee Plus invoices often do not have the company VAT ID… do not qualify for VAT reclaim… Invoices cannot be retroactively re-issued." Inbox Ledger also says "The billing@openai.com email is only a receipt; the tax invoice lives in the Stripe portal." ([Inbox Ledger, 2026](https://www.inboxledger.app/blog/how-to-download-chatgpt-plus-invoices))
- UK reverse charge: "We've been paying a monthly fee to OpenAI… the invoice makes no mention of VAT. Should I be paying reverse charge VAT?" Followed by "'someone' was dopey enough not to have remembered… last VAT quarter" (3.5k views) ([QuickFile](https://community.quickfile.co.uk/t/reverse-charge-vat-on-chatgpt/60395)). There is also an "Adding Italian VAT Id format not accepted" thread ([OpenAI forum](https://community.openai.com/t/adding-italian-vat-id-format-not-accepted/1380289)) and Japan JCT on OpenAI invoices since 2025 ([help](https://help.openai.com/en/articles/10242647-the-japanese-consumption-tax-on-your-openai-invoices)).
- Prices differ by tax regime: "€23 for Plus ($20 in the US)" ([13k.eu](https://13k.eu/en/ai-assistants/chatgpt-pricing/)). "B2B customers with a valid EU VAT ID can apply reverse-charge VAT treatment, paying €19…" *(excerpt)* ([aisubscriptioncomparison](https://aisubscriptioncomparison.com/pricing/ai-subscriptions-eu)).
- [INFERENCE] aicm fills "list price… and estimated past fees automatically" from a catalog. If those prices are US net prices, EU and UK users will see fees 19–25% below what their card was charged, unless tax-inclusive local prices or a per-account tax rate is applied.

**How often:** Medium for individuals; High for EU/UK businesses and teams.

**Competitors that have it:** Inbox Ledger, Spendbox and Track-Subs (email capture: [Spendbox](https://spendbox.co/), [Track-Subs](https://www.track-subs.com/)), Firefly III (attachments). Wallos has no tax handling.

**Size:** M.
- Charge `tax_amount`, `tax_rate` and `invoice_number`, plus file attachments stored in Postgres or on a volume.
- Per-account country, VAT ID and reverse-charge settings.
- A "missing invoice" check against expected renewals.
- Optional later: an IMAP/forwarding inbox that parses receipt emails into suggested charges (M–L).

### 10. Bill reconciliation: charged vs expected
**What users ask for:** flag when the money charged does not match what aicm expected:
- a renewal at a different price, or a duplicate charge;
- a charge after cancellation;
- an invoice far from usage × price;
- wallet spend (snapshot delta) diverging from token estimates.

**Evidence**
- From the OpenAI billing-problem tag index ([tag](https://community.openai.com/tag/api-billing-problem/552)):
  - "received 3 invoices for the same month… that don't match at all with my usage";
  - "On my API usage page it shows I have spent 1.7 USD… but in my general billing…";
  - "I only used ~70k tokens, and was charged for 4.06$";
  - "I cancelled the subscription two months ago. Despite this GPT keeps charging me monthly";
  - "My credit balance appears stuck even with usage".
- Codex: "Codex Credits Balance Does Not Match Usage History" *(excerpt)* ([OpenAI forum](https://community.openai.com/t/buying-credits-did-not-restore-my-codex-access-or-work-capacity/1397808)). r/openrouter: "Why did OpenRouter bill 4M tokens when OpenCode showed only 70K" *(excerpt)* ([r/openrouter](https://www.reddit.com/r/openrouter/comments/1u2ue4m/why_did_openrouter_bill_4m_tokens_when_opencode)).
- Pricing changes surprise people: Cursor's mid-2025 change led to "we will refund any unexpected charges" ([Cursor](https://cursor.com/en-US/blog/june-2025-pricing), [TechCrunch](https://techcrunch.com/2025/07/07/cursor-apologizes-for-unclear-pricing-changes-that-upset-users)). Bugbot moved from seats to usage billing in 2026 ([Cursor](https://cursor.com/blog/may-2026-bugbot-changes)).

**How often:** Medium–High. Each incident is individual, but they recur constantly.

**Competitors that have it:** None in the self-hosted space. Finance tools such as Inbox Ledger ("Missing invoices flagged before month-end") come closest.

**Size:** M. aicm uniquely has both sides of the comparison (money-out ledger and token estimates). Rules can be: renewal charge ≠ plan price ± tax; two charges of the same kind in one period; a charge on an account with `auto_renew=false` after the end date; month invoice vs usage estimate off by more than X%.

### 11. Idle subscriptions and seat utilization ("cancel candidates")
**What users ask for:** spot paid seats or subscriptions with little or no use in N days, and overlapping subscriptions, before renewal.

**Evidence**
- Reddit *(excerpts)*:
  - "I spent $340 on AI subscriptions last month… Claude Pro (40), ChatGPT Plus (20), Cursor (20), Perplexity Pro (20), Notion AI (10)…" ([r/ClaudeAI](https://www.reddit.com/r/ClaudeAI/comments/1tq6qth/i_spent_340_on_ai_subscriptions_last_month_wrote));
  - "$957 on AI subs this month. should i just consolidate" ([r/ChatGPTPro](https://www.reddit.com/r/ChatGPTPro/comments/1to9rx2/957_on_ai_subs_this_month_should_i_just));
  - "I suspect I'm not getting full value out of all of them — some overlap" ([r/ChatGPTPro](https://www.reddit.com/r/ChatGPTPro/comments/1wx22tf/people_juggling_multiple_ai_subscriptions_your/));
  - "Should I be paying for both Claude Code and Cursor?" ([r/cursor](https://www.reddit.com/r/cursor/comments/1nydkes/sorry_stupid_question_should_i_be_paying_for_both)).
- Flowstate sells this exact feature: "Find seats nobody is using… Inactive account ChatGPT — No activity for 45 days… Review seats before the contract renews… Request changes by 1 Oct" ([Flowstate](https://www.flowstate.inc/platform/ai-tools)). Claude Team credit pools are seat-based ([docs](https://platform.claude.com/docs/en/about-claude/api-credits-for-subscribers)).

**How often:** High for individuals stacking subscriptions; Medium for teams.

**Competitors that have it:** Flowstate (enterprise), Rocket Money (cancel flows), Wallos (AI "recommendations").

**Size:** S.
- An "idle" alert: no usage events or meter movement for N days on a subscription renewing within M days.
- A "cancellation notice deadline" field on accounts.
- quota_waste covers windows but not whole idle subscriptions.

### 12. Scheduled spend digests (weekly/monthly email or chat)
**What users ask for:** a periodic summary of month-to-date spend, the top movers, upcoming renewals, expiring credits and budgets at risk.

**Evidence:** LiteLLM "Weekly & Monthly spend reports per Team/Tag", `spend_report_frequency` ([docs](https://docs.litellm.ai/docs/proxy/alerting)); Helicone "Get regular cost summaries delivered to your inbox or Slack channels" ([docs](https://docs.helicone.ai/guides/cookbooks/cost-tracking)).

**How often:** Medium.

**Competitors that have it:** LiteLLM and Helicone.

**Size:** S. The SMTP and Notifier already exist; this is a River cron job.

### 13. Home Assistant, Prometheus and Grafana outputs
**What users ask for:** expose aicm numbers as Prometheus metrics or HA sensors, so homelab dashboards and automations can use them.

**Evidence**
- Unofficial HA integrations built just for AI limits and costs:
  - "Claude Usage" (58★/20 forks: session, weekly and extra-usage sensors) ([repo](https://github.com/trickv/hass-claude-usage));
  - Anthropic Admin API cost sensors with budget automations ([repo](https://github.com/burren2025/HAAnthropicUsageMonitor));
  - "OpenAI Codex Usage" sensors ([README](https://raw.githubusercontent.com/thematrixdev/home-assistant-codex-usage/master/README.md));
  - [hacs-claude-stats](https://github.com/dbpecka/hacs-claude-stats).
- Grafana and Prometheus stacks for Claude Code cost: [claudefana](https://github.com/JuanjoFuchs/claudefana), [claude-otel](https://github.com/sergiman94/claude-otel) and [Grafana dashboard 25255](https://grafana.com/grafana/dashboards/25255-claude-code-metrics-prometheus). Claude Code emits OTel cost metrics natively ([docs](https://code.claude.com/docs/en/monitoring-usage)). LiteLLM exposes `litellm_spend_metric` ([docs](https://docs.litellm.ai/docs/proxy/prometheus)).

**How often:** Medium. These are many small projects, each with modest stars.

**Competitors that have it:** LiteLLM (Prometheus) and the HACS integrations above. Wallos has no metrics endpoint.

**Size:**
- S: `/metrics` (Prometheus) with meters, balances, MTD spend and budget percentage, gated by an API token.
- M: a HACS integration, or MQTT discovery so entities appear in HA automatically.

### 14. Cloud-marketplace AI billing: AWS Bedrock, Google Vertex, Azure Foundry
**What users ask for:** when Claude or Gemini is bought through a cloud account, pull that spend in too.

**Evidence:** the AWS posts above ([Builder Center](https://builder.aws.com/content/2iQjuDjUHNYpgvRtFF5G9LpcmoC/tracking-claude-code-usage-on-amazon-bedrock-by-user-or-team), [Bedrock cost attribution guide, 2026-10](https://builder.aws.com/content/3JSAQyX69JuBJZoHbThKlg7Y8jS/amazon-bedrock-cost-attribution-a-prescriptive-guide)). Claude Code docs have sections for Bedrock, Google Cloud's Agent Platform and Microsoft Foundry ([costs](https://code.claude.com/docs/en/costs)). Claude Max API credits "can't be used on" Bedrock, Google Cloud or Foundry ([docs](https://platform.claude.com/docs/en/about-claude/api-credits-for-subscribers)), so those bills are separate.

**How often:** Medium for teams; Low for individuals.

**Competitors that have it:** CodexBar "AWS Bedrock — … Cost Explorer spend, monthly budgets" and "Vertex AI" ([README](https://github.com/steipete/CodexBar)).

**Size:** M per cloud (Cost Explorer `GetCostAndUsage` filtered to Bedrock; GCP billing export or Budgets API; Azure Cost Management). [INFERENCE] The agent may already see Bedrock and Vertex usage in Claude Code logs, so the main gap is money out.

### 15. Ingest self-hosted gateways and chat UIs: LiteLLM, Open WebUI
**What users ask for:** self-hosters who already run LiteLLM or Open WebUI want those per-user and per-key spend numbers in the same place.

**Evidence:**
- Open WebUI: "i want to limit the usage of users… as a word i want a usage management tools". The answers point to a community filter and a third-party token-tracking library ([discussion #24407](https://github.com/open-webui/open-webui/discussions/24407)). Community cost-tracker functions exist ([open-webui-cost-tracker](https://github.com/bgeneto/open-webui-cost-tracker)).
- awesome-selfhosted lists a Go gateway with "USD cost tracking, budgets" ([listing](https://awesome-selfhosted.net/tags/generative-artificial-intelligence-genai.html)).

**How often:** Medium on r/selfhosted and r/LocalLLaMA.

**Competitors that have it:** CodexBar "LiteLLM — Virtual key + proxy URL for personal and team budget/spend tracking", plus Bifrost and sub2api ([README](https://github.com/steipete/CodexBar)).

**Size:** S–M per adapter. LiteLLM has `/key/info`, `/user/info` and spend endpoints [INFERENCE: endpoint names to confirm].

### 16. iCal feed of renewals, trials and credit expiries
**Evidence:** Wallos exposes "Get Subscription iCal Feed" in its API ([API docs](https://api.wallosapp.com/)) and has a calendar view ([README](https://github.com/ellite/Wallos)). r/selfhosted *(excerpt)*: "view them on a calendar, and see weekly, monthly, and yearly totals" ([thread](https://www.reddit.com/r/selfhosted/comments/1fqvuvn/subscription_manager_selfhosted_app_to_track)).

**How often:** Medium (parity).

**Size:** S. Use a tokenized `.ics` URL and escape CRLF; Wallos had CVE-2026-77353 for ICS injection ([OpenCVE](https://app.opencve.io/cve/CVE-2026-77353)).

### 17. Mobile PWA and translations
**Evidence:** Wallos ships "Mobile view", a PWA manifest with shortcuts, recent "pwa: offline mode" fixes and "Multi Language support" ([README](https://github.com/ellite/Wallos), [CHANGELOG](https://github.com/ellite/Wallos/blob/main/CHANGELOG.md), [manifest](https://raw.githubusercontent.com/ellite/Wallos/v4.9.5/manifest.json)). CodexBar is "Localized… 23-language catalog" ([README](https://github.com/steipete/CodexBar)).

**How often:** Medium (parity).

**Size:** S for a PWA manifest, icons and installability; mobile layout of the current UI is unverified [INFERENCE]. M for i18n scaffolding.

### 18. Local-inference cost: electricity and hardware amortization
**Evidence:** "For a 4090 at full tilt (~350W)… At EU prices ($0.30-0.40/kWh) it's even more… Does anyone here actually factor electricity… into their compute decisions?" ([ollama #16339](https://github.com/ollama/ollama/issues/16339)). Also an "€ per million tokens, measured" article ([Towards Data Science](https://towardsdatascience.com/how-much-does-it-actually-cost-to-run-a-local-llm-e-per-million-tokens-measured)) and a LocalLLaMA TCO breakdown ([aiweekly](https://aiweekly.co/alerts/localllama-dev-breaks-down-64k-llm-server-tco)). CodexBar tracks "Neuralwatt… subscription kWh usage".

**How often:** Low–Medium (r/LocalLLaMA niche).

**Size:** S as a manual "local machine" account: kWh price × measured or assumed watts × hours, shown next to API spend.

### 19. FOCUS-format export
**Evidence:** the FinOps Foundation promotes FOCUS for AI usage ("SKU IDs indicating Token charges, Consumed Units specifying Tokens"), and Nebius and Snowflake already emit it ([FinOps for AI](https://www.finops.org/framework/technology-categories/ai/)).

**How often:** Low for aicm's audience.

**Size:** S (a CSV column mapping).

---

## Already covered: demand confirmed (do not re-propose)

| aicm feature | Demand evidence |
| --- | --- |
| Renewal reminders and cancel links | Wallos's core notification job and `sendcancellationnotifications` cron ([README](https://github.com/ellite/Wallos)); Flowstate "Review seats before the contract renews… Request changes by 1 Oct" ([Flowstate](https://www.flowstate.inc/platform/ai-tools)); trial trackers ([Gravity](https://cancelsubscriptionsapp.com/best/best-trial-trackers)). *Small add:* a cancellation-notice deadline (#11). |
| Low-balance alerts and balance history from snapshots | "Low-balance webhook alerts" and a balance endpoint are requested because OpenAI exposes neither ([OpenAI forum](https://community.openai.com/t/access-billing-balance-costs-via-admin-api/1383654)); a CLI keeps a manual ledger because "Neither OpenAI nor Anthropic exposes prepaid credit balance via API" ([llm-org-cost-monitor](https://github.com/yang3kc/llm-org-cost-monitor)); the HA integration estimates remaining credit ([repo](https://github.com/burren2025/HAAnthropicUsageMonitor)). |
| Quota windows (5-hour/weekly, Codex) | claude-code #44328 ("Monitor usage across accounts without logging into each one"), HA integrations and CodexBar. |
| Quota-waste alert | Claude Max/Team monthly API credits "don't roll over" ([docs](https://platform.claude.com/docs/en/about-claude/api-credits-for-subscribers)). |
| Multi-currency with dated FX | Wallos currency conversion ([README](https://github.com/ellite/Wallos)); codeburn "subscription tracking in 162 currencies" ([docs](https://codeburn.app/docs)); Inbox Ledger notes that Plus is charged in local currency ([post](https://www.inboxledger.app/blog/how-to-download-chatgpt-plus-invoices)). |
| Sync-failing alert | "Auto-recharge silently failed" ([OpenAI forum](https://community.openai.com/t/auto-recharge-silently-failed/1385922)). |
| MCP server | Someone built [wallos-mcp](https://github.com/mkpoli/wallos-mcp) to put an MCP server on a subscription tracker. |
| Roles, per-account grants, audit log | OpenRouter orgs show "every member's usage metadata to every member" ([blog](https://openrouter.ai/blog/insights/governing-team-ai-spend/)); PLANKA commenters list audit logging as a must-not-paywall feature ([#1754](https://github.com/plankanban/planka/issues/1754)). |
| Backups | Wallos added "backup and restore" in 1.23.0 ([CHANGELOG](https://github.com/ellite/Wallos/blob/main/CHANGELOG.md)); aicm already has nightly dumps. |
| REST API and tokens | Wallos API ([docs](https://api.wallosapp.com/)). |
| CSV export | Cursor users ask for "a CSV similar to 'Team Usage Events' but that included the repository" ([forum](https://forum.cursor.com/t/per-repository-usage-tracking-for-cost-attribution/154687)); OpenRouter Activity export. |
| Data stays local | Wallos: "your financial data remains private and secure on your own server"; codeburn: "Everything runs locally"; CodexBar's privacy note. |
| Docker, multi-arch, self-update | Expected baseline (Wallos Docker/Compose; Proxmox community script). |

## Notes for Main
- Plan decisions in conflict with this evidence:
  - SSO/OIDC is "Not planned" (#8).
  - Chat and push channels are framed as hosted features (#3).
  - Budgets are deferred until after MVP (#1).
- Fastest high-impact wins [INFERENCE]: webhooks plus ntfy (#3), credit-expiry lots (#4), a month-to-date budget alert (#1), mapping `extra_usage` (#6), the idle-subscription alert (#11) and `/metrics` (#13). Each is S or S–M and reuses existing tables and the Notifier.
- Biggest differentiators no self-hosted competitor has:
  - anomaly detection from top-up frequency and burn (#2);
  - charge-vs-expected reconciliation (#10);
  - per-project allocation from agent logs (#5).
