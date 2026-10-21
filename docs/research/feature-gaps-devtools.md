# aicm feature-gap research: AI coding subscriptions and usage trackers

Scope: people using Claude Code (Pro/Max), Codex/ChatGPT, Cursor, Copilot, Gemini/Antigravity, opencode, Z.ai/GLM, Kimi and OpenRouter, and the tools they use to track them (CodexBar, ccusage, tokscale, codeburn, Claude-Code-Usage-Monitor, ccstatusline, claude-swap/codex-swap, Claude Tuner, claumon, Nowdex, CodexBar iOS fork).
Research date: 2026-10-21. Sources: GitHub issues (counts from the GitHub API at fetch time; `r` = reactions, `c` = comments), HN threads (Algolia API), product pages and READMEs, Reddit and X through search-result snippets (Reddit blocked direct fetches with 403/429), and vendor docs.
Sizes: S = a few days, M = 1–2 weeks, L = multi-week. [INFERENCE] marks my own reasoning, not something a source states.

---

## Ranked missing features

### 1. Banked "limit reset" inventory, expiry alerts, and "use it now or save it" advice (Claude + Codex)
**What users ask for:** Both vendors now hand out one-off resets that expire: Codex "banked resets" and Claude "limit resets". Users want to see every reset they hold, when each one expires, an alert before one lapses, and advice on whether to spend one now.
- openai/codex#28161 "Show expiration dates for each available usage reset" (60r, open): "Please show the expiration time for each available usage reset." https://github.com/openai/codex/issues/28161
- openai/codex#27915 "Linux Codex users cannot access or redeem banked usage resets" (41r). https://github.com/openai/codex/issues/27915
- Bugs where resets vanished before their displayed expiry: openai/codex#50508 https://github.com/openai/codex/issues/50508 and #51646 ("expire at least one hour earlier than the expiration time shown") https://github.com/openai/codex/issues/51646
- CodexBar#1758 "Show all Codex reset-credit expiration times, not only the next one". https://github.com/steipete/CodexBar/issues/1758
- Claude Help Center, "What is a limit reset?": "An unused reset expires on the day and time listed on the offer… The 'Reset for free' button isn't currently available… in Claude Code in your terminal." https://support.claude.com/en/articles/17007452-what-is-a-limit-reset
- @ClaudeDevs on X: "your reset is available today in Settings → Usage. Apply it any time until Oct 22." https://x.com/ClaudeDevs/status/2102438803013333469
- r/codex Reset Discussion Megathread (snippet): "A banked reset refreshes your usage, but the current behaviour also pushes your weekly reset date forward… Surprise global resets can also wipe out unused capacity." https://www.reddit.com/r/codex/comments/1uu2c1g/reset_discussion_megathread/
- Claude Tuner (claims 20,000+ users) sells this as a headline feature: "Reset passes: use it now, or save it?" https://claudetuner.com
- Claude Code#76272 asks for user control over the weekly reset, "or banked on-demand resets like Codex" (36r). https://github.com/anthropics/claude-code/issues/76272

**How often:** High, and rising since mid-2026. Six-plus issues with 40–60 reactions, megathreads, and a vendor X post about a reset that expires tomorrow (2026-10-22).
**Competitors:** CodexBar shows the Codex count plus the next expiry only. Claude Tuner shows passes with use-or-save advice. Neither is self-hosted or multi-account.
**aicm size:** S for the meter, expiry and an "expiring reset" alert. M for use-or-save advice inside `recommend_account`/`forecast_capacity`. The advice would weigh days until the weekly reset, how exhausted the window is, and the fact that redeeming a reset moves the weekly anchor. [INFERENCE] Codex data may come from `codex app-server` or `wham/usage`. Claude data needs the OAuth usage endpoint, because the statusline payload does not include resets.

### 2. "Quota is back" notifications, including surprise global resets
**What users ask for:** A ping when a window they had exhausted resets, so they can resume work. A separate ping when a vendor resets everyone's limits early.
- CodexBar#263 (6r): "I would love to be notified when my Codex quota is reset… I'd like to get notified about both resets, but only if I am out of quota at the moment." https://github.com/steipete/CodexBar/issues/263
- openai/codex#21073 "Auto-resume CLI session when usage limit resets" (86r, open): "If that reset time falls while I'm asleep, the information is wasted — the task just dies." https://github.com/openai/codex/issues/21073
- Claude Code#79127 "Suspend and auto-resume at usage-limit reset" https://github.com/anthropics/claude-code/issues/79127 and #41502 "--wait-on-usage-limit flag" (5r, 17c). https://github.com/anthropics/claude-code/issues/41502
- Global resets are frequent and big news. @ClaudeDevs, "We've reset 5-hour and weekly rate limits for all users" (7.7M views): https://x.com/ClaudeDevs/status/2077603834453770467 . Codex: https://x.com/thsottiaux/status/2061106703446450392 . r/ClaudeAI (snippet): "Anthropic dropped a surprise usage limit reset for everyone, reset like 10 hours early." https://www.reddit.com/r/ClaudeAI/comments/1uxs0qt/usage_limit_was_just_reset
- Standalone services exist only to send reset alerts by Telegram or SMS: https://resetalerts.com/codex-28-day-challenge and https://reset-tracker.com/

**How often:** High. The Codex auto-resume issue alone has 86 reactions.
**Competitors:** CodexBar has optional session-quota notifications. Claude Desktop auto-continues after a reset (https://code.claude.com/docs/en/whats-new/2026-w33). Reset Alerts sends announcements only, not per-account alerts.
**aicm size:** S. Add an "available again" alert type for when a meter drops after being at or near 100%. Add an "unexpected reset" alert for when a meter falls well before `resets_at`. [INFERENCE] aicm already stores meter snapshots, so both are diffs over stored data. Auto-resuming sessions is out of scope, but a webhook (gap 3) lets users wire it up themselves.

### 3. Push and chat notification channels, plus a mobile-first PWA
**What users ask for:** Alerts on the phone, delivered through channels other than email, and a way to check quotas away from the desk without handing credentials to a phone app.
- CodexBar#4430: "I want to check my provider usage from my phone or another device without logging into each provider on that device… Provider credentials should remain on the Mac." https://github.com/steipete/CodexBar/issues/4430 . Related: #439 iOS companion (5r) https://github.com/steipete/CodexBar/issues/439 , #4029 mobile sync https://github.com/steipete/CodexBar/issues/4029 , #1976 iOS LAN mirror https://github.com/steipete/CodexBar/issues/1976 .
- Claude Code#61157 "Live Usage Limit Tracker Widget — iOS & Android". https://github.com/anthropics/claude-code/issues/61157
- A market has formed: Nowdex (iOS/iPad/Mac/Windows) https://nowdex.app/ ; a CodexBar iOS fork with quota alerts and widgets https://apps.apple.com/kz/app/codexbar-usage-of-ai/id6760216772 ; Starbridge (Android/web) and Notify! (iPhone Live Activities), both listed in the CodexBar README https://github.com/steipete/CodexBar ; CodexBar-android https://news.ycombinator.com/item?id=47250848 ; "Multi-provider iOS usage alerts" https://news.ycombinator.com/item?id=47036656 .
- Security pain with phone apps. r/ClaudeAI (snippet): to use one iPhone widget app "you have to grant it OAuth access… users point out is full access, not just a read-only." https://www.reddit.com/r/ClaudeAI/comments/1uxla8x/i_made_some_iphone_widgets_to_make_claude_codes
- Chat channels in adjacent tools: Claude Window Starter has Telegram control https://github.com/aetsr/claude-code-window-starter/blob/main/docs/CLAUDE-CODE-5-HOUR-WINDOW.md ; CCS ships `ccs config channels` for Telegram/Discord/iMessage https://www.vividkit.dev/guides/ccs ; Reset Alerts runs on Telegram.

**How often:** High. Many separate apps exist only to fill this gap.
**Competitors:** Nowdex, the CodexBar iOS fork, Starbridge, Claude Tuner (browser).
**aicm size:** S for webhook, ntfy, Telegram, Discord, Slack and Pushover delivery on the existing alert engine. [INFERENCE] A Go notifier library such as shoutrrr covers most of these. M for an installable PWA with Web Push and a phone-first Quotas view. A self-hosted hub that keeps credentials on the server answers the security complaint above.

### 4. Provider and log-source coverage for coding plans aicm doesn't read yet
**What users ask for:** One tracker covering every coding plan they pay for. Requests by demand signal:
- **Cursor** (included pools plus on-demand spend). The "Cursor Stats" extension has 13,548 installs https://marketplace.visualstudio.com/items?itemName=Dwtexe.cursor-stats . HN: "Something like this would be great for Cursor, I never know how many credits I'm up to for the month" https://news.ycombinator.com/item?id=44317012 . Cursor's own dashboard got worse: "since July 31 the Usage page shows tokens, not dollars… now it isn't even a balance you can read" https://devaireviews.com/tools/pricing/cursor . r/cursor "Beware of Cursor's on-demand billing trap" https://www.reddit.com/r/cursor/comments/1u9vwv8/beware_of_cursors_ondemand_billing_trap_my_20_pro .
- **Antigravity / Gemini.** Gemini CLI was replaced by Antigravity CLI for unpaid and Google One users on 2026-06-18 https://geminicli.com/docs/resources/faq/ . CodexBar#129 (11r) https://github.com/steipete/CodexBar/issues/129 ; CodexBar#4533 "read Gemini app usage limits (5h/weekly)" https://github.com/steipete/CodexBar/issues/4533 ; ccusage#1402 https://github.com/ccusage/ccusage/issues/1402 ; many single-purpose quota extensions https://github.com/topics/quota-monitor .
- **Kimi Code plan** (weekly, 5-hour and monthly quota; aicm only reads the Moonshot API balance). The "Kimi Usage" VS Code extension has 5,047 installs https://marketplace.visualstudio.com/items?itemName=kayuii.kimi-usage ; endpoint `GET https://api.kimi.com/coding/v1/usages` https://raw.githubusercontent.com/sylearn/AIUsage/main/QuotaBackend/Sources/QuotaBackend/Providers/KimiProvider.swift .
- **Ollama Cloud**: CodexBar#534 (23r) https://github.com/steipete/CodexBar/issues/534 . **Muse Code**: CodexBar#3409 (20r) https://github.com/steipete/CodexBar/issues/3409 . **Windsurf**: CodexBar#128 (9r) https://github.com/steipete/CodexBar/issues/128 . **MiniMax, Alibaba coding plan, OpenCode Go**: supported by Nowdex and CodexBar, plus a crowd of opencode quota plugins https://github.com/slkiser/opencode-quota .
- **Local log sources** beyond Claude Code, Codex and omp: opencode ("OpenCode data not showing after sqlite migration", 33r) https://github.com/ccusage/ccusage/issues/845 ; Copilot CLI https://github.com/ccusage/ccusage/issues/1174 ; Cursor agent https://github.com/ccusage/ccusage/issues/1771 . tokscale reads 50+ clients https://github.com/junhoyeo/tokscale and codeburn reads 42 https://github.com/getagentseal/codeburn .

**How often:** Very high. This is the most common kind of issue on CodexBar and ccusage.
**Competitors:** CodexBar (106 providers), tokscale, codeburn, Nowdex.
**aicm size:** S each for API-key quota endpoints (Kimi Code, MiniMax, Ollama Cloud). M each for cookie or session-based ones (Cursor, Antigravity, Windsurf). S–M per local log parser (opencode SQLite, Gemini/Antigravity CLI, Copilot CLI). For a first release, Cursor and Antigravity matter most.

### 5. Wrong-wallet and extra-usage leak detection (subscription vs API vs overage)
**What users ask for:** Warning when work that should come out of a subscription is actually billed to an API key or to metered "extra usage", and live visibility of overage spend.
- Claude Code#62338 "Claude Code silently billed $447 to API instead of Max subscription" https://github.com/anthropics/claude-code/issues/62338 ; #58083 "silently uses project ANTHROPIC_API_KEY from .env instead of Max plan OAuth" https://github.com/anthropics/claude-code/issues/58083 . Anthropic's docs confirm an `ANTHROPIC_API_KEY` env var takes precedence over the subscription https://code.claude.com/docs/en/env-vars .
- A runaway-cost checklist collects $6,000 overnight runs, a $187 surprise from a stray key (#68406), and a $864 silent auto-purchase (#68285): "Others are on a paid subscription with quota to spare, yet watch metered 'Extra Usage' drain anyway." It also reports that since 2026-06-15 "interactive and programmatic usage bill to separate pools." https://gist.github.com/yurukusa/a0b66592fe016d3823b8090d25af1a18
- Claude Code#40707: "there is no visibility into how much money is being spent. The only notification is email receipts from Anthropic." https://github.com/anthropics/claude-code/issues/40707 ; #47111 "prompt before consuming extra usage credits" (5r) https://github.com/anthropics/claude-code/issues/47111 ; #45249 "Max 20x subscription ignored - Claude Code ONLY bills to [extra usage]" https://github.com/anthropics/claude-code/issues/45249
- HN: GitHub integration "spends from your API wallet balance" while on Max https://news.ycombinator.com/item?id=44317012
- Copilot after the move to metered billing: "1,180 credits used. 16% of my monthly Pro+ allowance. Gone." https://www.theregister.com/ai-and-ml/2026/06/02/github-copilot-users-threaten-exit-as-metered-billing-kicks-in/5249826

**How often:** High and costly. Recurring issues and incident write-ups.
**Competitors:** None ship this. cc-safe-setup (hooks) and codeburn guard are the nearest.
**aicm size:** M. [INFERENCE] aicm already discovers both subscriptions and API keys per machine. It could alert when a machine has `ANTHROPIC_API_KEY` or `OPENAI_API_KEY` set in the shell or a project `.env` while a subscription account exists there, or when API-key spend and subscription headroom rise at the same time. Tracking Claude extra-usage and Codex purchased credits as their own meters would also help, if they aren't already.

### 6. Cost and usage by project, git branch, session and subagent/MCP, plus cache-hit rate
**What users ask for:** Where the tokens went: which repo, which feature branch, which session, which subagent or MCP server.
- ccusage#281 "show usage for a specific project, and breakdown by project" https://github.com/ccusage/ccusage/issues/281 ; #1329 subagent breakdown: "trying to optimize my cc bills by first understanding which part of my flow eats most of the tokens" https://github.com/ccusage/ccusage/issues/1329 ; #1817 "Compare Codex usage and cost per conversation" https://github.com/ccusage/ccusage/issues/1817
- codeburn (11.5k stars, HN 112 points) cuts spend "by project, by git branch, by model and by task" and adds "Compare periods" https://github.com/getagentseal/codeburn . On HN: "Cache hit rate is another metric I wouldn't have looked at otherwise… Activity classification is the actually useful feature though." https://news.ycombinator.com/item?id=47759035
- dev.to comment: "I would rather see burn rate by workflow type than only a remaining quota number." https://dev.to/pederaa/claude-codes-weekly-limit-explained-and-how-to-see-it-coming-4a59 . Claude's own `/usage` now attributes usage to skills, subagents and MCP servers (same article).
- Per-repo account choice: claude-swap `cswap map` binds directories to accounts https://pypi.org/project/claude-swap/0.22.0 ; Claude Code#52793 "Per-project account switching" https://github.com/anthropics/claude-code/issues/52793

**How often:** High among cost-conscious users.
**Competitors:** codeburn, tokscale (Projects and Sessions views), CodexBar (project and session views in Usage & Spend).
**aicm size:** M. [INFERENCE] The agent already parses these logs, which carry the working directory and session IDs. Add project and session dimensions to Usage, plus a cache-read share column.

### 7. Unexplained-drain and silent-limit-change detection (meter vs actual tokens)
**What users ask for:** Proof when the meter moves without matching local usage (a background process, another device, a vendor bug), or when the vendor quietly shrinks the allowance.
- CodexBar#180 "CodexBar is clobbering my Claude 5x Max plan usage": "I find CodexBar uses a ton of my 5x Max plan, even when I'm not at my computer." https://github.com/steipete/CodexBar/issues/180
- r/ClaudeCode "Usage limit is consumed without using claude" https://www.reddit.com/r/ClaudeCode/comments/1vaaudm/usage_limit_is_consumed_without_using_claude
- Claude Code#28848 "Max plan usage limits silently reduced" (33r): "The actual limits are never published — just a vague percentage with no denominator… No notification when they change." https://github.com/anthropics/claude-code/issues/28848 ; #69430 "Weekly Limit Jumped From ~50% to 100% in less than an hour" https://github.com/anthropics/claude-code/issues/69430 ; #38910 "Spurious Usage Spikes" https://github.com/anthropics/claude-code/issues/38910
- openai/codex#41220 meta tracker (27r, 69c): "quota… depleted materially faster than the user's prior baseline or local/token evidence would predict… usage records that do not reconcile with the meter." https://github.com/openai/codex/issues/41220
- Community measurement projects: claude-quota-meter https://github.com/cwy433-png/claude-quota-meter ; "~21 BILLION tokens per weekly limit on the Max 20x" https://www.reddit.com/r/ClaudeAI/comments/1x4inan/21_billion_tokens_per_weekly_limit_on_the_max_20x/ ; wokey.ai's measured weekly value https://wokey.ai/claude-usage-limits
- HN (Grafana thread): "alerts for abnormal spikes" https://news.ycombinator.com/item?id=47584429

**How often:** High, and emotionally charged.
**Competitors:** No tracker reconciles the meter against tokens across machines.
**aicm size:** M. [INFERENCE] aicm uniquely has both the provider meter and per-machine token logs for the same account. A "tokens per 1% of window" series per account and model could drive two alerts: "meter rose N% with no matching local activity" and "your effective allowance dropped X% vs your 4-week baseline". This could be a headline differentiator.

### 8. Per-model and extra windows (Claude Fable/Opus weekly, Kimi monthly, Codex model scopes)
**What users ask for:** Separate meters for model-specific weekly caps, which often bind before the overall weekly cap.
- Claude Help: "Max plans also have a separate weekly limit for Fable." https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan
- Claude Code#73770 "statusline: expose per-model weekly rate limits (Opus/Sonnet/Fable)" (25r, open) https://github.com/anthropics/claude-code/issues/73770 . The statusline payload today only has `five_hour`, `seven_day` and (behind a gateway) `spend_limit` https://code.claude.com/docs/en/statusline .
- claude-swap `--model Fable` exists because "If you work on one model and hit its weekly per-model limit first…" https://pypi.org/project/claude-swap/0.22.0 . CodexBar maps `limits[].weekly_scoped` https://github.com/shaneholloman/codexbar/blob/main/docs/claude.md . claumon shows "per-model utilization straight from the Claude OAuth usage API" https://github.com/fabioconcina/claumon .

**How often:** Medium-high, and growing with each new premium model.
**Competitors:** CodexBar, claumon, claude-swap.
**aicm size:** S–M. [INFERENCE] If aicm only reads Claude limits from the statusline, it cannot see these windows. It would need the OAuth usage endpoint, used carefully (see the quality notes below).

### 9. Plan right-sizing and subscription value (API-equivalent value vs fee, upgrade/downgrade simulation)
**What users ask for:** "Am I on the right tier?" and "how much API value did my $200 buy?"
- Claude Tuner: "Stop Overpaying for Your Plan. Analyzes 30 days of usage to recommend the right plan… 3-level downgrade assessment" (claims 20,000+ users, 280+ organizations) https://claudetuner.com
- Claude-Code-Usage-Monitor#8: "it could also make recommendations like 'based on your usage last month, considering upgrading / downgrading to this plan / API'." https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/8 ; the burncheck tool in #167 shows "Which Max tier (20/100/200) you'll cross" https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/167
- HN: "My total tokens used… worth around $3397.34" on Max20; "I'm around ~2100 dollars equivalent on Max20 plan" https://news.ycombinator.com/item?id=44317012
- claude-swap#350: users cancel and resubscribe to match need, and want "when the plan ends; whether it will renew or whether it has been cancelled." https://github.com/realiti4/claude-swap/issues/350

**How often:** Medium-high.
**Competitors:** Claude Tuner (browser extension, Claude/ChatGPT/Gemini only).
**aicm size:** S–M. [INFERENCE] aicm already has list prices, fees and estimated API-equivalent cost. Missing are a value-per-dollar ratio per account and a plan simulation over 30 days of meter history. The cancelled-but-active status can come from Claude's OAuth profile (`subscription_status`, per the claude-swap#350 thread) to mute renewal alerts automatically.

### 10. Wallet runway: "your balance or credits run out on <date> at this pace"
**What users ask for:** A date and a month-end projection, not just a low-balance threshold.
- The Register on Copilot: "At this rate, my 7,000-unit quota will be depleted in less than two days." https://www.theregister.com/ai-and-ml/2026/06/02/github-copilot-users-threaten-exit-as-metered-billing-kicks-in/5249826
- Claude-Code-Usage-Monitor#128 (6r) asks for a date on the "Tokens will run out" prediction https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/128 . codeburn shows "what the month is on pace to cost" https://github.com/getagentseal/codeburn . Claude Tuner shows "Limit ETA" and "Expected wait" https://claudetuner.com .

**How often:** Medium.
**Competitors:** codeburn (month pace), Claude Tuner (quota ETA).
**aicm size:** S. [INFERENCE] `forecast_capacity` covers quota windows. Extend it to wallets (OpenRouter, DeepSeek, Copilot AI credits, Moonshot) and show it on Overview, Quotas and in alerts.

### 11. A standard glance surface: statusline segment, stable JSON endpoint, then tray/menu bar
**What users ask for:** Usage visible where they work (terminal statusline, desktop bar, Stream Deck, desk displays) without opening a dashboard.
- Claude-Code-Usage-Monitor#131 "Could you add status line support" (11r) https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/131 ; ccstatusline#94 "/usage support" (9r) https://github.com/sirmalloc/ccstatusline/issues/94
- HN: "I wish it was just a simple command that outputs how much I have used out of my plan already." https://news.ycombinator.com/item?id=44317012
- r/codex (snippet): "I have to look constantly at my limit to not start a big task with low 5h limit." https://www.reddit.com/r/codex/comments/1u0ueys/codex_limit_widget
- The CodexBar README lists 20+ community widgets (Waybar, KDE, GNOME, COSMIC, SketchyBar/tmux, Stream Deck, ESP32 desk display) built on `codexbar serve` and its JSON output https://github.com/steipete/CodexBar . Many HN "Show HN" menu bar apps: https://news.ycombinator.com/item?id=46956460 , https://news.ycombinator.com/item?id=48775727

**How often:** Very high (the menu bar is the dominant form factor).
**Competitors:** CodexBar, codeburn menubar/Capacity Dock, CCS Bar, claude-swap menubar.
**aicm size:** S for `aicm-agent statusline` printing the cross-account summary, and a documented token-scoped `/api/glance` JSON for community widgets. L for a native tray app. [INFERENCE] Ship the S parts first.

### 12. Usage-window scheduling ("start my 5-hour window before I sit down")
**What users ask for:** Open the 5-hour window early so its reset lands mid-workday.
- claude-swap#263: "The Claude 5 hour session countdowns don't start until you send/receive the first token under the fresh reset. It would be advantageous to automatically send something in each time the 5 hour session is detected to have reset." https://github.com/realiti4/claude-swap/issues/263
- Tools built only for this: claude-warmup https://github.com/vdsmon/claude-warmup , cc-prewarm https://www.npmjs.com/package/cc-prewarm , Claude Window Starter https://github.com/aetsr/claude-code-window-starter/blob/main/docs/CLAUDE-CODE-5-HOUR-WINDOW.md , and the Medium post "Stop Hitting Claude's Rate Limit at 11 AM" https://medium.com/@AymanAlkurdi/stop-hitting-claudes-rate-limit-at-11-am-7cad2b7db3d9
- HN: "so I should say hello to claude after waking up for an early reset 5h later" https://news.ycombinator.com/item?id=44317012

**How often:** Medium.
**Competitors:** Window Starter, plus warmup scripts.
**aicm size:** M. Policy risk: it spends real quota and is a gray area. [INFERENCE] Make it opt-in and agent-side, using real observed `resets_at`. At minimum, `recommend_account` could suggest "start now so the reset lands at 13:00".

### 13. Acting on the recommendation: per-repo account mapping and rotation hand-off
**What users ask for:** Not just "use account B" but making B active, or launching the session with B, plus strategies like drain-soonest-reset-first.
- claude-swap: "10K" weekly downloads per its PyPI page; auto-switch at a threshold, `consume-first` strategy ("perishable weekly quota isn't wasted"), per-directory mapping https://pypi.org/project/claude-swap/0.22.0 . claude-swap#382: "by mid-week 3 of my accounts are at 90-96% and only one has room left. Then all my sessions share that one 5h window." https://github.com/realiti4/claude-swap/issues/382
- Codex equivalents: codex-swap https://github.com/PavloPopravkin/codex-swap , codex-switch https://github.com/edwardgushchin/codex-switch . Cross-provider: CCS ("Hit a rate limit? Continue the same task in another provider") https://www.vividkit.dev/guides/ccs ; cux https://github.com/inulute/cux ; claude-max-overflow https://github.com/AaronBergman/claude-max-overflow
- CodexBar deliberately refused to route (#2173): "Recommending which provider to use next is a product direction CodexBar is not taking; it reports, it does not route." https://github.com/steipete/CodexBar/issues/2173
- Risks: r/ClaudeCode "Claude just banned having multiple Max accounts" (snippet: "ties usage to one main account") https://www.reddit.com/r/ClaudeCode/comments/1r7x2su/claude_just_banned_having_multiple_max_accounts/ ; Claude Code#41881 "Accounts on the same billing method share a single rate limit quota pool" https://github.com/anthropics/claude-code/issues/41881

**How often:** High among power users with several accounts.
**Competitors:** claude-swap, codex-swap, CCS, cux.
**aicm size:** L to own credential switching, with ToS risk. [INFERENCE] Do not swap credentials. Expose `recommend_account` as a CLI or env output (`aicm-agent recommend --tool claude --cwd .`) that cswap, cxswap and CCS can consume. Add per-directory preferred accounts. Model linked pools (#41881) so recommendations don't count two accounts as independent when they share a pool.

### 14. Prometheus `/metrics` and an OTLP receiver
**What users ask for:** Feeding Grafana, and covering machines without the agent (CI, cloud dev boxes) through Claude Code and Codex OpenTelemetry.
- HN "Monitor Claude Code Usage with Grafana": "you can also use standard alert management & notification tools to send updates on usage and alerts for abnormal spikes." https://news.ycombinator.com/item?id=47584429
- Several dashboards and stacks: https://grafana.com/grafana/dashboards/24993-claude-code-metrics , https://github.com/JuanjoFuchs/claudefana , https://github.com/acreeger/claude-code-metrics-stack ; Grafana Cloud Codex integration https://grafana.com/docs/grafana-cloud/observe-and-act/monitor-infrastructure/integrations/integration-reference/integration-openai-codex ; HN (Anthropic employee) on team trends via OTel https://news.ycombinator.com/item?id=44317012

**How often:** Medium, but especially relevant to self-hosters.
**Competitors:** DIY OTel stacks.
**aicm size:** S for `/metrics`, M for OTLP ingest.

### 15. Provider status and incident awareness ("outage, or am I out of quota?")
- CodexBar: "Provider status polling surfaces incident badges in the menu and an indicator overlay on the bar icon." https://github.com/steipete/CodexBar . Usage reporting itself breaks during incidents ("errors loading usage data") https://status.claude.com/
- Claude Code#61828 "shows 'Usage limit reached' despite session at 2% and weekly at 32%" (7r, 21c) https://github.com/anthropics/claude-code/issues/61828

**How often:** Medium.
**aicm size:** S (poll Statuspage feeds; badge on Quotas; suppress "sync failing" noise during a vendor incident).

### 16. Spend guards on local agents (warn or stop at $X; confirm before overage)
- codeburn: "`codeburn guard install` adds Claude Code hooks that warn a session once it passes $5 and stop it at $15." https://github.com/getagentseal/codeburn
- Claude Code#47111 "prompt before consuming extra usage credits" https://github.com/anthropics/claude-code/issues/47111 ; #68346 "Team plan: ask the member to confirm before switching into extra-usage" https://github.com/anthropics/claude-code/issues/68346 ; the $6,000 overnight-run checklist https://gist.github.com/yurukusa/a0b66592fe016d3823b8090d25af1a18

**How often:** Medium.
**aicm size:** M. [INFERENCE] This moves aicm from advisory to enforcement, so keep it opt-in and agent-side.

### 17. Team views for shared or pooled usage
- Claude Tuner's team dashboard ("Who holds passes"), used by 280+ organizations https://claudetuner.com ; Claude Code#67772 "Team/org-level /stats view" https://github.com/anthropics/claude-code/issues/67772 ; #90152 / #92517 asks to pool or gift usage https://github.com/anthropics/claude-code/issues/92517 ; "Cursor Team Usage" extension https://marketplace.visualstudio.com/items?itemName=JinYongWuGit.cursor-usage-team ; claumon: "Anthropic's usage analytics dashboard is for Team and Enterprise org admins; it is not available to individual Pro or Max plans" https://github.com/fabioconcina/claumon

**How often:** Medium.
**aicm size:** S–M. [INFERENCE] aicm already has users, groups and machines. What's missing is a "who consumed this shared meter" breakdown that joins machine token logs to account meters.

### 18. Shareable cards and "Wrapped" (low priority, but a growth lever)
- tokscale leaderboard and Wrapped https://github.com/junhoyeo/tokscale ; CodexBar iOS share cards https://apps.apple.com/kz/app/codexbar-usage-of-ai/id6760216772 ; Year in Code https://news.ycombinator.com/item?id=46401120 ; Claude Tuner "Shareable usage cards & yearly Wrapped".

**aicm size:** S. [INFERENCE] This drives virality but doesn't solve a cost problem.

---

## Quality bars learned from competitor complaints (anti-requirements)
- **No repeated Keychain prompts.** This is CodexBar's most-reacted bug class: #108, #186, #340, #2115, #624, #1991, around 17–35r each. https://github.com/steipete/CodexBar/issues/340 , https://github.com/steipete/CodexBar/issues/2115
- **Stay light and dense.** CodexBar#1364 "becoming bloated" (37r): "We want an OVERVIEW in the freaking overview!!" https://github.com/steipete/CodexBar/issues/1364 ; lag issues #1274 (35r) and #1321 (34r).
- **Polling must not consume the user's quota.** See CodexBar#180 above. Spawning `claude` triggers paid "warmup" usage per that thread. CodexBar#1894 notes that ~20 OSS projects probe `anthropic-ratelimit-unified-*` headers by sending real (tiny) inference calls. https://github.com/steipete/CodexBar/issues/1894
- **Expect windows to appear and disappear.** Codex temporarily removed its 5-hour limit (openai/codex#34035, 279r) https://github.com/openai/codex/issues/34035 . Banked resets move the weekly anchor (r/codex megathread above).

## Possible gaps to verify against aicm's implementation
- Multiple Claude/Codex profiles on one machine (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`): ccusage#66 https://github.com/ccusage/ccusage/issues/66 , Claude-Code-Usage-Monitor#116 https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/116 , CodexBar#1843 (17r) https://github.com/steipete/CodexBar/issues/1843
- Codex archived sessions counted (`~/.codex/archived_sessions`): ccusage#880 (5r) https://github.com/ccusage/ccusage/issues/880
- Historical pricing (price at usage date, not today's price): ccusage#764 https://github.com/ccusage/ccusage/issues/764
- Codex purchased credits shown separately from plan windows (the CodexBar iOS changelog lists it); Claude Console credit grants with expiry (same changelog).
- Windows agent support: Windows forks of CodexBar, "Claude Usage Monitor for Windows" https://news.ycombinator.com/item?id=49930607 , codeburn Windows tray.
- Timezone correctness of resets: Claude-Code-Usage-Monitor#121 (9r) https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/121
- Pace and burn-down visible in the web UI, not just through MCP. CodexBar shows "X% in reserve / Runs out in … / Lasts until reset" and a weekly-vs-5h verdict (#2261).

---

## Already covered by aicm, with demand confirmed
| aicm capability | Evidence that users want it |
|---|---|
| Several accounts per provider | CodexBar#81 (28r, 32c) https://github.com/steipete/CodexBar/issues/81 ; #1843 (17r): "I want N accounts of the same provider visible at once, without switching between them" https://github.com/steipete/CodexBar/issues/1843 ; #1268 (14r) https://github.com/steipete/CodexBar/issues/1268 ; Claude-Code-Usage-Monitor#94 https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/94 |
| Hub aggregating many machines | ccusage#222 (8r, open) "Accurate… Tracking Across Multiple Devices" https://github.com/ccusage/ccusage/issues/222 ; #287 "sync daemon so multiple machines can push logs to a single hub" https://github.com/ccusage/ccusage/issues/287 |
| Weekly and 5-hour meters with resets | Claude-Code-Usage-Monitor#167 (20r): "I still have to go to the Claude web UI to check my Weekly usage" https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/167 ; openai/codex#1986 (151r) https://github.com/openai/codex/issues/1986 ; Claude Code#20636 (59r) https://github.com/anthropics/claude-code/issues/20636 |
| `recommend_account` (which account to use now) | CodexBar#2173 asked for it and was declined ("it reports, it does not route"), so this differentiates aicm https://github.com/steipete/CodexBar/issues/2173 ; a commenter's tip: "rank by pace, used% / elapsed fraction of the window, not by remaining %" |
| "Quota going to waste" alert | CodexBar#2261: "I hate leaving weekly unused because I've exceeded my 5 hours" https://github.com/steipete/CodexBar/issues/2261 ; claude-swap `consume-first` |
| "Meter nearly exhausted" alerts | Reddit menu bar app with "Proactive notifications at 70% and 90%" https://www.reddit.com/r/ClaudeAI/comments/1qekqwp/beta_built_a_macos_menu_bar_app_to_track_claude ; Claude Tuner 80/95% alerts |
| Copilot AI credits | Metered billing backlash (The Register, above); GitHub added per-cycle credit visibility https://github.blog/changelog/2026-07-20-copilot-users-can-now-see-ai-credits-used-per-billing-cycle/ |
| Z.ai coding plan windows | Z.ai ships an official usage-query plugin https://docs.z.ai/devpack/extension/usage-query-plugin ; CodexBar#133 (8r) https://github.com/steipete/CodexBar/issues/133 |
| Claude limits via statusline | Claude Code#15366 (41r), #18121 (38r), #27915 (34r) all asked for this, and Anthropic shipped the `rate_limits` fields https://code.claude.com/docs/en/statusline |
| MCP server | codeburn and kimi-code-usage both ship MCP servers https://github.com/getagentseal/codeburn , https://pypi.org/project/kimi-code-usage |
| Multi-currency | Claude-Code-Usage-Monitor#132 "Different currencies" https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor/issues/132 |
| Per-model breakdown | ccusage#342 (Opus and Sonnet shown separately) https://github.com/ccusage/ccusage/issues/342 |
| Capacity forecast | claumon's calibrated forecasts; Claude-Code-Usage-Monitor#128 |
