# ChangeLog for Asika

## v20260511DEV > Unleased

### Phase 1 — Immediate Features

- **Webhook health check + polling fallback**: New `webhook_health` bbolt/MongoDB bucket. `GET /api/v1/webhooks/health` returns per-repo-group/platform status (last seen, healthy/unhealthy). Background health checker worker runs every 2 minutes; if no webhook received within threshold (default 5m), forced polling is automatically enabled for the affected repo group. Configurable via `[events]` section (`health_check_interval`, `health_check_threshold`).

- **Notification quiet hours + role-based escalation**: New `[quiet_hours]` TOML section. During quiet hours, non-urgent notifications are suppressed or routed to escalation contacts based on role (`admin`|`operator`). Urgent event types (e.g. `spam_detected`, `sync_failed`) bypass quiet hours. Timezone-aware with configurable start/end times. New `SendNotificationUrgent` API bypasses quiet hours.

- **Config dry-run + auto-rollback**: New `POST /api/v1/config/dry-run` endpoint validates a config patch without applying it, returning the merged config with secrets masked. Auto-rollback watches for 60 seconds after config update; if DB health check fails, automatically rolls back to the previous snapshot version. `CurrentCfgVersion()` exported for version tracking.

- **Team metrics dashboard**: New `GET /api/v1/stats/team` endpoint with per-author aggregation (PRs opened/merged/reviewed, avg lead time). New WebUI page (`/team`) with Chart.js bar chart for top contributors and sortable authors table. `TeamStats` and `AuthorStats` models.

- **Scheduled report enhancement**: Reports now include per-repo-group breakdown, per-platform stats, and top contributors from team stats API. Generated reports are stored in new `report_history` bucket with timestamp and period. New `GET /api/v1/reports` API and WebUI page (`/reports`) for viewing report history. `ScheduleConfig.PeriodDays` added.

- **`asika watch` CLI command**: New `watch` subcommand with `prs`, `stats`, and `team` sub-commands. Polls the server at configurable intervals and displays live terminal output with ANSI color codes and screen refresh. Supports `--interval` flag.

### Phase 2 — Mid-term Features

- **Merge condition expressions**: New `Expression` field in `[merge_queue]` config. Supports expressions like `approvals >= 2 AND ci == success AND NOT conflict`, `author IN core_contributors`, `age_hours > 24`, `label IN labels` with AND/OR/NOT and parentheses. Custom expression evaluator (no external dependency). Falls back to original hardcoded logic when no expression is configured.

- **PR auto-labeling/assignment enhancement**: Label rules now support `Priority` (higher = evaluated first) and `Exclusive` (stop after first match). Per-repo-group label rules via `label_rules` in `[[repo_groups]]` — group rules are merged with global rules. Review rules also support `Priority`.

- **Audit log enhancement**: Enhanced `AuditLog` model with `Category`, `Actor`, `RepoGroup`, `PRNumber`, `Platform`, `Action`, `Before`/`After` fields. New `AppendAuditLogEx` API for structured audit entries. Log filtering by `category`, `actor`, `repo_group`, `action`, `since`. CSV export includes new columns.

- **Notification preference center + dedup**: New `notification_prefs` and `notification_dedup` buckets. Per-user notification preferences via `GET/PUT /api/v1/users/:username/notifications`. Preferences include enabled notifiers, event type toggles, digest mode, and per-user quiet hours override. Notification deduplication with 5-minute TTL per event+PR+notifier combination.

- **Team space isolation**: New `TeamSpace` and `SpaceMember` models. Team spaces group repo groups with isolated member management via `space_admin`/`space_operator`/`space_viewer` roles. REST API at `/api/v1/spaces` for CRUD. WebUI at `/spaces`. New `team_spaces`, `space_members`, `space_settings` buckets.

### Previous Features

- **Revert PR**: All 5 platform clients (GitHub, GitLab, Gitea, Bitbucket, Gerrit) implement `RevertPR`. Revert creates a new PR, adds to merge queue, auto-merges, comments on original PR, and sends notification. New `POST /api/v1/repos/:rg/prs/:id/revert` endpoint and `pr revert` CLI command.
- **Close reasons**: Configurable close reasons via `[close_reasons]` TOML section. Closing a PR with a reason auto-creates and applies a label. Supported in REST API (`?reason=` param), CLI (`--reason` flag), WebUI (modal dialog), and all 4 bots.
- **Spam author tracking**: New `spam_authors` bbolt/MongoDB bucket. Marking a spam PR records the author with count, first/last seen timestamps. `SpamAuthor` model with full CRUD.
- **State-based PR actions**: PR action buttons are now state-dependent: open PRs show Approve/Close/Spam; closed/spam PRs show Reopen; merged PRs show Revert. Applied to WebUI, all 4 bots, and CLI.
- **Permission model**: Added `CanRevert` to `UserPermissions` and `RequirePermission("revert")` middleware case.
- **Event system**: Added `EventPRReverted` and `EventNotificationEscalated` event types.
- **Refactor**: Split monolithic `daemon/handlers/prs.go` (1065 lines) into `daemon/handlers/pr/` sub-package with 7 files: `pr.go` (shared vars + ListPRs/GetPR), `approve.go`, `close.go`, `reopen.go`, `comment.go`, `label.go`, `logs.go`. Thin wrapper in `prs.go` preserves backward compatibility.
- Add `min_procs` / `max_procs` server config for GOMAXPROCS control at startup and runtime hot-reload.
- WebUI settings page adds CPU Threads section for min_procs and max_procs.
- All platform bots (Telegram, Discord, Slack, Feishu) display CPU thread config in /config command.
- Add notification channel fault alerting: when a notifier fails 3 consecutive times, an alert is sent through all other configured notifiers (excluding the failed one).
