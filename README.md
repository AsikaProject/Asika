# Asika

Asika ([/əˈsiːkə/](https://ipa-reader.com/?text=əˈsiːkə), pronounced *uh-SEE-kuh*) is a portmanteau of
**Akira** (明, "bright, intelligent") and **seeker**.
Like an intelligent seeker, it scans your repositories, finds pull requests,
detects spam, and applies labels — keeping everything clear and under control.

## Why Asika?

Managing pull requests across multiple platforms is messy.

You switch between GitHub, GitLab, Gitea, Forgejo, Codeberg, or Bitbucket, keep dozens of tabs open, and still risk merging too early or missing important changes.

**Asika fixes this by giving you a single control plane to manage, automate, and safely merge PRs — without leaving your workflow.**

### 🔹 One place for everything
No more tab-switching. See and manage PRs across platforms from one dashboard or chat.

### 🔹 Safe merges, not early merges
Built-in merge queue ensures PRs are only merged when approvals and CI checks are complete.

### 🔹 Automate the boring parts
Labels, stale PRs, and repetitive actions are handled automatically.

### 🔹 Works where you already are
Approve, close, or check PRs directly from chat via Telegram, Feishu (Lark), Discord, or Slack.

### 🔹 Fine-grained access control
Three roles (viewer/operator/admin) plus six granular permissions (approve, merge, close, reopen, spam, queue) with per-user repo group isolation.

### 🔹 API Key authentication
Long-lived API keys for CI/CD and external integrations. Keys are bound to a role (admin/operator/viewer) with optional granular permissions. Create and manage keys via WebUI (`/apikeys`), bot commands (`/apikey new/list/revoke`), or CLI (`asika apikey`). Keys are delivered via DM and auto-deleted after 2 minutes.

### 🔹 Simple to run
Single binary. No Node.js. No external dependencies. Embedded bbolt database by default, with optional MongoDB support.

## Quick Start

### 1. Get a binary

Download from [releases](https://github.com/minibp/asika/releases) or build it:

```bash
git clone https://github.com/minibp/asika.git
cd asika

# Build with strip (default, version: YYYYMMDDDEV)
bash build.sh

# Or use specific commands:
bash build.sh build     # Build binaries (default, stripped)
bash build.sh dep       # Download dependencies
bash build.sh test      # Run all tests
bash build.sh clean     # Remove build artifacts
bash build.sh distclean # Deep clean (includes Go cache)
```

Binaries: `asika` (CLI) and `asikad` (daemon). Version is auto-generated from date.

### 2. Configure

First time? Run the wizard:

```bash
./asika wizard
```

Or fire up the daemon and use the web wizard at `http://localhost:8080`:

```bash
sudo ./asikad
```

Minimal config (`/etc/asika_config.toml`):

```toml
[server]
listen = ":8080"

[tokens]
github = "ghp_xxx"

[[repo_groups]]
name   = "my-project"
github = "org/repo"
```

**GitHub Enterprise Server** — set `github_base_url` to your GHE API URL:

```toml
[server]
github_base_url = "https://github.example.com/api/v3"

[tokens]
github = "ghev_xxx"
```

See `asika.toml.example` for the full reference — it covers notifications, spam detection, label rules, user management, and more.

### 3. Start managing

```bash
# Login
./asika login

# CLI
./asika pr list my-project

# User management
./asika user add alice --password secret --role operator --groups my-project
./asika user update alice --role admin --permissions approve,close,queue

# Or open the dashboard
# http://localhost:8080
```

## Chat Bots

### Slack

Use natural language commands in any channel the bot is invited to:

```
prs my-project        → List PRs
pr my-project 42      → Show PR #42
approve my-project 42 → Approve
close my-project 42   → Close
reopen my-project 42  → Reopen
spam my-project 42    → Mark as spam
queue my-project      → Check merge queue
recheck my-project    → Trigger recheck
config                → Show config summary
help                  → All commands
```

### Telegram

Start a chat with your bot:

```
/prs my-project        → List PRs
/pr my-project 42      → Show PR #42
/approve my-project 42 → Approve
/close my-project 42   → Close
/reopen my-project 42  → Reopen
/spam my-project 42    → Mark as spam
/queue my-project      → Check merge queue
/recheck my-project    → Trigger recheck
/config                → Show config summary
/help                  → All commands
```

### Feishu (Lark)

Send messages directly to the bot:

```
prs my-project    → List PRs
pr my-project 42  → Show PR #42
approve my-project 42 → Approve
close my-project 42   → Close
spam my-project 42    → Mark as spam
queue my-project      → Check queue
recheck my-project    → Trigger recheck
config                → Show config
help                  → All commands
```

### Discord

Use slash commands or prefix commands in your Discord server:

```
!prs my-project        → List PRs
!pr my-project 42      → Show PR #42
!approve my-project 42 → Approve
!close my-project 42   → Close
!reopen my-project 42  → Reopen
!spam my-project 42    → Mark as spam
!queue my-project      → Check merge queue
!recheck my-project    → Trigger recheck
!config                → Show config summary
!help                  → All commands
```

Or use Discord slash commands: `/prs`, `/pr`, `/approve`, `/close`, `/spam`, `/queue`, etc.

## CLI Cheatsheet

All commands need a token: `asika --token <token>` or set `ASIKA_TOKEN`. Login once with `asika login` to save the token.

```bash
# Authentication
asika login                    # Login and save token
asika wizard                   # Interactive setup wizard

# PR operations
asika pr list [group]          # --state open|closed|merged, --platform github|gitlab|gitea|forgejo|codeberg|bitbucket
asika pr show [group] [id]     # PR details
asika pr approve [group] [id]  # Approve PR
asika pr close [group] [id]    # Close PR
asika pr reopen [group] [id]   # Reopen PR
asika pr spam [group] [id]     # Mark/unmark spam (--undo)
asika pr comment [group] [id] [body]  # Comment on PR
asika pr rebase [group] [id]   # Rebase PR onto base branch
asika pr cherry-pick [group] [id] <target-branch>  # Cherry-pick merged PR

# Batch operations
asika pr batch-approve [group] [id1,id2,...]  # Batch approve PRs
asika pr batch-close [group] [id1,id2,...]    # Batch close PRs
asika pr batch-label [group] [id1,id2,...] --label <name> [--color <hex>]

# Merge queue
asika queue list [group]       # Show queue
asika queue recheck [group]    # Trigger recheck
asika queue clear [group]      # Clear all queue items
asika queue remove <group> <id>  # Remove specific PR

# Scheduled merge
asika pr schedule-merge [group] [id] --at "2026-05-11T14:00:00+08:00"  # Schedule a PR merge

# API Key management
asika apikey create <name> <role>   # Create API key (admin only)
asika apikey list                   # List all API keys (admin only)
asika apikey revoke <key_id>        # Revoke an API key (admin only)
asika login --api-key <key>         # Save API key for CLI use

# User management
asika user list                # List all users
asika user add <username> --password <pwd> --role <role> [--groups <g1,g2>] [--permissions <p1,p2>]
asika user update <username> [--password <pwd>] [--role <role>] [--groups <g1,g2>] [--permissions <p1,p2>] [--no-perms <p1,p2>]
asika user delete <username>   # Delete user

# Roles: admin, operator, viewer
# Permissions: approve, merge, close, reopen, spam, queue

# Label rules
asika rules list               # List label rules
asika rules add <pattern> <label>   # Add label rule
asika rules remove <pattern>       # Remove label rule

# Stale PR management
asika stale check [group]      # Check for stale PRs (--dry-run)
asika stale unmark [group] [id]  # Remove stale label

# Sync (multi mode)
asika sync history             # Show sync history (--repo_group, --limit)
asika sync retry [sync_id]     # Retry failed sync

# Stats
asika stats                    # Show DORA metrics and overview

# Self-update
asika self-update              # Update to latest version
asika self-update --check      # Check for updates
asika self-update --rollback   # Rollback to previous version
asika self-update --dry-run    # Preview without making changes

# Config
asika config show              # Show current config (secrets masked)
asika config set --file <path> # Update config from TOML file
asika config reload            # Hot reload config

# Watch (live terminal monitor)
asika watch prs [group]        # Watch PR changes (--interval 10)
asika watch stats              # Watch DORA metrics and queue status
asika watch team               # Watch team contributor stats

# Version
asika version                  # Show CLI version
asika version server           # Show server version
```

## Configuration Highlights

### Database

By default Asika uses an embedded bbolt database. MongoDB is also supported:

```toml
# bbolt (default)
[database]
type = "bbolt"
path = "/var/lib/asika/asika.db"

# MongoDB
[database]
type = "mongo"
path = "mongodb://localhost:27017"
name = "asika"
```

### Repo Groups

**Multi Mode** — Sync PRs across platforms:

```toml
mode = "multi"  # default

[[repo_groups]]
name           = "my-project"
github         = "org/repo"
gitlab         = "org/repo"
gitea          = "org/repo"
default_branch = "main"
```

**Single Mode** — One platform only:

```toml
mode = "single"

[single_repo]
platform       = "github"
repo           = "org/repo"
default_branch = "main"
```

### User Management

Create users with fine-grained permissions and repo group access:

```toml
[[users]]
username = "alice"
password = "bcrypt-hash-here"
role = "operator"
allowed_repo_groups = ["frontend", "backend"]

[users.permissions]
can_approve = true
can_close = true
can_manage_queue = true
can_merge = false
can_reopen = false
can_spam = false
```

Roles: `admin` (full access), `operator` (inherits viewer + operations), `viewer` (read-only).
Admins bypass all permission and repo group checks.

### Bot UID Permissions (TOML-based)

For platforms where users don't have DB accounts (e.g., Telegram numeric IDs), you can grant bot-level permissions directly in TOML:

```toml
[telegram]
admin_ids    = [123456789]
operator_ids = [987654321]
viewer_ids   = [111222333]

[discord]
admin_ids    = ["discord_user_id_1"]
operator_ids = ["discord_user_id_2"]

[feishu]
admin_ids    = ["ou_admin_open_id"]
operator_ids = ["ou_operator_open_id"]

[slack]
admin_ids    = ["U_ADMIN_SLACK_ID"]
operator_ids = ["U_OPERATOR_SLACK_ID"]
```

When all three lists are empty, the bot is open to everyone (backward compatible).
When only `admin_ids` is set, only those users are admins — everyone else is rejected.
Users in `operator_ids` can use operator-level commands (PR operations, list users).
Users in `viewer_ids` can only use read-only commands.

### Label Rules

Auto-label by file patterns (glob or regex):

```toml
[[label_rules]]
pattern = "**/*.go"
label   = "go"

[[label_rules]]
pattern = "docs/**"
label   = "documentation"

[[label_rules]]
pattern = "regex:^.*test.*$"
label   = "has-tests"
```

Rules are hot-reloadable — edit and they apply without restart.

### Spam Detection

Catch bad PRs automatically:

```toml
[spam]
enabled  = true
threshold = 3           # max PRs per time window
time_window = "10m"      # lookback window
trigger_on_author = true
trigger_on_title_kw = ["spam", "fix typo", "readme update"]

# Auto-clean: periodically clear keywords and reset author trigger
auto_clean_enabled = false
auto_clean_interval = "24h"
```

### Spam Keyword Management (WebUI)

Spam keywords can be managed individually in the WebUI settings page:
- Add keywords one at a time with Enter key or Add button
- Remove individual keywords with ✕ on each tag
- Select multiple keywords with checkboxes and batch delete
- Auto-clean periodically resets the keyword list and author trigger

### Quiet Hours

Suppress or escalate notifications during off-hours:

```toml
[quiet_hours]
enabled          = false
start_time       = "22:00"
end_time         = "08:00"
timezone         = "Asia/Shanghai"    # empty = local time
escalation_role  = "admin"            # role to notify during quiet hours: "admin" | "operator"
bypass_for_urgent = ["spam_detected", "sync_failed"]  # event types that bypass quiet hours
```

When quiet hours are active, non-urgent notifications are suppressed. Urgent events (spam, sync failures) bypass quiet hours. Escalation role determines which notifier types remain active.

### CPU Thread Control

Control the Go runtime's OS thread count via `[server]`:

```toml
[server]
min_procs = 0   # min OS threads; 0 = Go default (1), otherwise floor
max_procs = 0   # max OS threads; 0 = use all CPUs (NumCPU)
```

Both are hot-reloadable at runtime via `PUT /api/v1/config` or the WebUI settings page. When both are non-zero, `max_procs` must be >= `min_procs`.

### Notifications

Get alerts where you work. Supports multiple channels simultaneously — just add more `[[notify]]` blocks.

```toml
# Email
[[notify]]
type   = "smtp"
config = { host = "smtp.example.com", port = 587, username = "bot@example.com",
           password = "xxx", to = ["team@example.com"] }

# Microsoft Teams — Incoming Webhook
[[notify]]
type   = "msteams"
config = { webhook_url = "https://outlook.office.com/webhook/xxx/IncomingWebhook/yyy" }

# Slack — Bot API
[[notify]]
type   = "slack_bot"
config = { token = "xoxb-your-bot-token", channel_id = "#general" }

# WeCom — Webhook mode (single URL)
[[notify]]
type   = "wecom"
[notify.config]
webhook_url = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"

# WeCom — Webhook mode (multiple URLs + @mentions + textcard)
[[notify]]
type   = "wecom"
[notify.config]
webhook_urls = ["https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=yyy"]
mentioned_list = ["zhangsan", "lisi"]
mentioned_mobile_list = ["13800138000"]
msg_type = "textcard"  # "markdown" (default), "textcard", "text"

# WeCom — App mode (proactive push to users/parties/tags)
[[notify]]
type   = "wecom"
[notify.config]
corp_id = "wwxxxxxxxxxx"
corp_secret = "xxxxxxxx"
agent_id = 1000001
to_user = ["zhangsan", "lisi"]
msg_type = "textcard"

# DingTalk
[[notify]]
type   = "dingtalk"
[notify.config]
webhook_url = "https://oapi.dingtalk.com/robot/send?access_token=xxx"
secret = "SECxxx"  # optional, for HMAC-SHA256 signature
at_mobiles = ["13800001111"]  # optional, @specific users by mobile
at_all = false  # optional, @everyone
msg_type = "markdown"  # "text" (default), "markdown", "link", "actionCard", "feedCard"

# GitHub @mentions
[[notify]]
type   = "github_at"
config = { owner = "org", repo = "repo", to = ["admin1", "admin2"] }

# Telegram
[[notify]]
type   = "telegram"
config = { token = "bot-token", to = ["@channel", "123456789"] }

# Feishu/Lark
[[notify]]
type   = "feishu"
config = { webhook_url = "https://open.feishu.cn/open-apis/bot/v2/hook/xxx",
           app_id = "cli_xxx", app_secret = "xxx" }
```

### Notification Digest

Rapid events on the same PR are batched into a single digest notification to reduce spam. The first event sends immediately; subsequent events within a 5-minute window are combined into a summary:

```
📋 PR uuid-123: 3 events
  • approved
  • ci_passed
  • labeled ×2
```

### Fault Alerting

When a notification channel fails to send 3 consecutive times, Asika automatically sends a fault alert through all other configured notifiers. Each channel tracks failures independently, and the counter resets on the next successful send.

Alert message format:
```
[Fault Alert] Notifier telegram failed 3 consecutive times

Notifier type: telegram
Consecutive failures: 3
Last error: <error details>

Please check this notifier's configuration and connectivity.
```

## WebUI Features

- **Dashboard** — DORA metrics, overview stats, PR breakdowns by repo group/platform, recent activity
- **Team Stats** — Per-author contribution metrics, top contributors chart, sortable authors table
- **Bottleneck Analysis** — Identifies reopened, long-review, stale, and frequently-rejected PRs with P90/P95 lead time percentiles
- **Scheduled Merge** — Schedule a PR to merge at a future time via the merge queue
- **PR Management** — List, detail view, approve, close, reopen, spam/mark, comment, rebase, cherry-pick
- **Merge Queue** — View queue status, recheck, clear, remove individual items
- **Reports** — View generated report history with per-group and per-platform breakdowns
- **System Usage** — Real-time CPU/memory monitoring with auto-refresh, GOMEMLIMIT tracking, color-coded progress bar
- **User Management** — Create, edit, delete users with role and permission assignment, repo group access control
- **Settings** — Merge queue config, spam detection (keyword tags with batch operations), stale PR management, label rules editor, CPU thread control (min_procs / max_procs), config history with rollback
- **API Keys** — Create, list, revoke API keys with role and permission config; keys delivered via DM with 2-minute auto-delete
- **Config** — Raw TOML editor, dry-run validation, system info, self-update, stale PR check
- **Webhook Health** — Per-repo-group/platform webhook status monitoring with automatic polling fallback
- **i18n** — English/Chinese language switcher (cookie-based, instant apply). Default is English.
- **PWA** — Installable, works offline with service worker

## Contributing

We'd love your help! For first contributing, see [contributing guide](./CONTRIBUTING.md) first.

## License

BSD 3-Clause — see [LICENSE.md](LICENSE.md) for details.

## Issues?

Found a bug? Want a feature? [Open an issue](https://github.com/minibp/asika/issues).

For detailed technical docs, see `PROJECT.md` (for developers) and `asika.toml.example` (for configuration).

See [ChangeLog](./ChangeLog.md) for the full release history.
