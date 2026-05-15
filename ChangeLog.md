# ChangeLog for Asika

## v20260510DEV > Unreleased

- **Feature**: Added HMAC-based fingerprint authentication. Configurable via `[auth] fingerprint_enabled/secret/expiry`. Endpoints: register, verify, list, revoke. Middleware reads `X-Fingerprint-Token` header or `Authorization: Fingerprint <token>`.

- **Bug fix**: `extractToken` (server) did not validate `Bearer` prefix on Authorization header, accepting any scheme (e.g. `Basic`). Now requires `Bearer` prefix, consistent with common middleware.

- **Bug fix**: `UpdateUser` missing `CanRevert` permission field in request struct and update logic. Added `can_revert` to the permissions JSON binding and DB save path.

- **Performance**: `ValidateAPIKey` now uses HMAC-SHA256 pre-check to avoid O(n) bcrypt comparisons. Only keys with matching HMAC proceed to bcrypt verification. API keys now store `key_hmac` field.

- **Bug fix**: `CleanupBlacklist` was never called periodically, causing unbounded memory growth. Added hourly cleanup worker in `StartWorkers` that also cleans expired fingerprints.

- **Bug fix**: `handlePRReopened` called `SyncOnMerge` with bare `c.ctx` (no timeout), inconsistent with `handlePRMerged`. Added 10-minute timeout.

- **Bug fix**: `recordSync` called `db.Put` directly, bypassing the writer actor. Added `SyncRecordWriter` interface to Syncer and wired it through the consumer's writer actor for serialized bbolt writes.

- **Bug fix**: `AppendAuditLogEx` (bbolt) set Timestamp after Marshal, causing zero-time in stored JSON. Moved zero-check before Marshal.

- **Bug fix**: `AppendAuditLogEx` (MongoDB) set Timestamp after bson.Marshal and cast ObjectID to string via `.(string)` which panics. Fixed zero-check and used `.(bson.ObjectID).Hex()`.

- **Bug fix**: Consumer `Start()` leaked goroutine on restart — old writer/worker pool never stopped. Added `Stop()` call at start.

- **Bug fix**: Webhook retry worker `StopWebhookRetryWorker()` double-close panics. Protected with `sync.Mutex` + `select`.

- **Bug fix**: Syncer `recordSync` silently dropped json.Marshal errors. Added error handling and logging.

- **Bug fix**: `EventPRLabeled` only logged but didn't persist label changes to DB. Added `handlePRLabeled` handler.

- **Bug fix**: Gerrit `GetFileContent` ignored HTTP error status codes and body read errors. Added status code check and error handling.

- **Bug fix**: Notifier `mustMarshal` silently dropped json.Marshal errors, corrupting dedup entries. Returns nil on error, callers check.

- **Bug fix**: MongoDB `ListNotificationPrefs` filtered on non-existent `username` field instead of `_id`. Fixed filter.

- **Bug fix**: MongoDB `ensureIndexes` returned on first failure, skipping remaining indexes. Made per-index with warn logging.

- **Bug fix**: Event bus subscribers never removed — SSE reconnect leaked channels. Added `Unsubscribe()` wired into SSE handler.

- **Bug fix**: Labeler `compiledPatterns` map had concurrent read/write data race. Protected with `sync.RWMutex`.

- **Bug fix**: `SyncOnMerge` goroutines used `context.Background()` with no timeout. Added 10-minute timeout.

- **Bug fix**: Config hot-reload had lost-update race (concurrent PUT) and non-atomic file write. Added `sync.Mutex` and atomic tmp+rename.

- **Bug fix**: Feishu bot HTTP response body leaked in fire-and-forget goroutine. Added `resp.Body.Close()`.

- **Bug fix**: CLI `pr comment` and batch commands used `fmt.Sprintf` for JSON — injection if input contains quotes. Replaced with `json.Marshal`.

- **Bug fix**: MongoDB `AppendAuditLogEx` index entry errors silently dropped. Added `slog.Error` logging.

- **Performance**: `isNotifierEnabledForAnyUser` did full DB scan on every notification. Added 30s TTL cache.

- **Performance**: `GetIssuePRLinksByPR` and `GetPRDependentsByPR` did full table scan. Added reverse index buckets.

- **Performance**: MongoDB added indexes on `issue_pr_links.pr_id` and `pr_dependencies.depends_on_pr_id`.

## v20260510DEV > Unreleased

- **WebUI PR detail enhancements**: PR detail page now shows PR body/description, last commit SHA, and audit log events. Added Rebase, Cherry-pick, and Comment action buttons directly on the detail page.

- **WebUI batch operations**: PR list page now supports batch approve, batch close, and batch label operations via checkboxes.

- **Bot PR detail enhancements**: All platform bots (Telegram, Discord, Feishu, Slack) now display PR description and audit log events in the PR detail view. Telegram, Feishu, and Slack additionally provide Rebase and Cherry-pick inline buttons.

- **Slack rebase/cherry-pick**: Implemented actual rebase and cherry-pick functionality for Slack bot (previously returned "not yet implemented").

- **Consumer Events**: Webhook events (opened/closed/merged/reopened/spam) now auto-append to `PR.Events` for display in PR detail views.

- **GetLogs pr_number filter**: `GetLogs` endpoint now supports `pr_number` query parameter for filtering audit log entries by specific PR, using the `audit_log_index` bucket for efficient lookup.

- **Bug fix**: `VerifyNotifiers()` — empty Gerrit webhook secret now correctly rejects all webhooks (previously silently accepted all).

- **Bug fix**: `consumer.go` — 5x `json.Marshal` errors now properly checked and logged (previously silently discarded).

- **Bug fix**: Event bus `Publish` is now non-blocking — slow subscribers no longer block all event processing. Dropped events are logged with warn.

- **Bug fix**: `writerActor` write() now checks the stop channel, preventing deadlock on `Stop()`.

- **Bug fix**: `prepareRepoDir` now returns an error to the caller (previously ignored).

- **Bug fix**: 2x `rand.Read` failures now logged with `slog.Error` before returning (previously silently propagated).

- **Bug fix**: Webhook retry worker now has a proper `Stop()` method with stop channel, preventing goroutine leak on shutdown.

- **Bug fix**: `TestPublishBackpressure` rewritten for non-blocking event bus behavior.

- **Branch sync**: New `branch_sync` config option in `[[repo_groups]]`. Set to `"all"` to sync every branch (not just default) from source to all target platforms. Uses `git for-each-ref` to enumerate branches and force pushes each one. Branch sync runs after the default branch sync completes.

- **Tag sync**: New `sync_tags = true` config option in `[[repo_groups]]`. When enabled, all tags from source platform are synced to all target platforms. Tags are immutable so no conflict resolution needed — simple force push of `refs/tags/*`.

- **Bare repo cache**: When `[git] repo_clone_path` is configured, syncer uses persistent bare repositories instead of temp dirs. Bare repos are cloned once with `git clone --bare`, then updated with `git fetch` on each sync. Eliminates repeated full clones and dramatically speeds up sync.

- **Force push for cross-platform sync**: `gitutil.Push()` now uses `Force: true` in push options. Cross-platform sync is a deterministic overwrite operation — the goal is to make all platforms match the source. Force push ensures sync succeeds regardless of target branch state. `pushWithRetry` now only retries on transient errors (network issues), not conflicts.

- **Sync conflict auto-retry**: `cherryPickWithRetry` and `pushWithRetry` now retry up to 3 times with exponential backoff (2s, 4s, 8s) on conflict errors. Before each retry, re-fetches from source to get latest changes. Conflict detection via `isConflictError()` checks for "conflict", "non-fast-forward", "rejected", "failed to push" in error messages.

- **Sync conflict notification**: New `SetNotifyFunc` on `Syncer` wires into the notifier system. When sync fails (cherry-pick or push), sends a notification with PR title, source/target platforms, and failure reason. Wired in both `workers.go` (handler syncer) and `consumer.go` (event consumer syncer).

- **Sync event publishing**: `SyncOnMerge` now publishes `EventSyncFailed` on the event bus when sync fails (partial or complete), and `EventSyncCompleted` on full success. Enables downstream consumers to react to sync status.

- **Bitbucket/Gerrit sync targets**: `getTargetPlatforms` now includes Forgejo, Codeberg, Bitbucket, and Gerrit as sync targets (previously only GitHub/GitLab/Gitea). All 7 platforms are synced to when configured.

- **Sync branch deletion retry**: `SyncBranchDeletion` now retries up to 3 times with exponential backoff on transient errors (timeout, connection refused, rate limit, etc.).

- **Scheduled report cron enhancement**: Replaced `cronToInterval()` with `github.com/robfig/cron/v3` for real cron expression support. Named schedules (`hourly`, `daily`, `weekly`, `monthly`) map to standard cron shortcuts (`@hourly`, `@daily`, etc.). Custom cron expressions like `0 9 * * 1` (every Monday 9am) are now supported. Invalid expressions fall back to `weekly` with a warning. New `slogCronLogger` bridges cron's logger interface to `log/slog`.

- **Bug fix**: `GetRepoGroupByName` and `GetRepoGroups` now correctly map the `Gerrit` field from `RepoGroupConfig` to `RepoGroup` (previously silently dropped).

- **Config auto-rollback notifier health check**: `VerifyNotifiers()` pings each configured notifier after a config update; if all notifiers fail, auto-rollback triggers alongside the existing DB health check.

- **SSE event streaming**: New `GET /api/v1/events` SSE endpoint (`daemon/handlers/events.go`) subscribes to the event bus and streams PR events in real-time with 30s heartbeat. New `asika watch stream` CLI subcommand connects to the SSE endpoint for live updates without polling.

- **Team space access control**: New `RequireSpaceAccess()` middleware checks if the user is a member of the team space that owns the requested repo group. Applied to all PR routes after `RequireRepoGroupAccess`. Space membership resolved via `TeamSpace.RepoGroups` and `SpaceMember` records.

- **Audit log secondary index**: New `audit_log_index` bucket with prefix-based secondary indexes by actor, repo_group, action, and category. `AppendAuditLogEx` writes index entries on every log write. `GetLogs` uses index for filtered queries, falling back to full scan when no filter specified.

- **Audit log Before/After tracking**: State-changing handlers (approve, close, reopen, mark_spam) now populate `AuditLog.Before` and `AuditLog.After` fields with PR state diffs (e.g. state, labels, is_approved, spam_flag).

- **Notification preference center WebUI**: New `notifications.html` template and `GET /notifications` route for per-user notification management. `sendNotificationInternal` now checks `isNotifierEnabledForAnyUser()` which iterates `NotificationPreferences` to skip notifiers disabled by all users.

- **PR auto-assignment enhancement**: Review rules now support per-repo-group configuration via `[[repo_groups.review_rules]]` in TOML. Group rules are merged with global rules (group rules take precedence, sorted by priority). New `POST /api/v1/repos/:rg/prs/:id/assign` endpoint for manual reviewer assignment (requires `approve` permission). New `POST /api/v1/repos/:rg/prs/:id/codeowners-assign` endpoint re-evaluates CODEOWNERS and assigns reviewers. CODEOWNERS parser (`daemon/reviewer/codeowners.go`) fetches from standard locations, uses GitHub-style last-match-wins semantics, and caches parsed results with 5-minute TTL.

- **Repo-level permissions**: New `AllowedRepos []string` field on `User` and `APIKey` models (format: `"owner/repo"`). New `RequireRepoAccess()` middleware resolves the actual repo from the PR record and checks against the user's allowed repos list. Both JWT and API key authentication support per-repo access control. Empty `AllowedRepos` = access to all repos (backward compatible).

- **RSS feed subscription**: New `daemon/feed/` package with in-memory ring buffer (default 50 items) consuming PR events from the event bus. `GET /api/v1/feed.xml` returns RSS 2.0 XML feed; append `?repo_group=<name>` to filter by repo group. `GET/PUT /api/v1/feed/config` for admin configuration. Configurable via `[feed]` TOML section (`enabled`, `title`, `max_items`, `public_feed`).

- **Documentation**: Update PROJECT.md with reviewer auto-assignment, RSS feed, repo-level permissions sections; update architecture diagram with Feed module; add Reviewer and Feed package descriptions.

### High-Difficulty Features

- **Serial merge validation**: New `SerialWorker` in `daemon/queue/serial_worker.go` runs an independent validation queue before merge. State machine: `validating → rebasing → waiting_ci → ci_running → ready → merging`. Each PR is rebased onto latest `main`, force-pushed, and CI is re-validated before marking merge-ready. Prevents "multiple PRs pass CI individually but fail after merge" problems. New `serial_queue` bucket and `QueueItem.ValidationStatus`/`ValidationDetail` fields. `RebaseAndPush` added to `common/gitutil`.

- **Cross-team PR collaboration**: New `cross_space_deps` bucket. When a PR is merged, `NotifyCrossSpaceDeps` checks `PRDependency` records for cross-space dependents. Publishes `EventSyncCompleted` notification to downstream PRs in other spaces with rebase instructions. REST API: `GET /api/v1/repos/:rg/prs/:id/cross-space-deps`, `GET /api/v1/cross-space-deps/:source/:target`, `POST /api/v1/cross-space-deps/:source/:target/resolve`.

- **Role-based tiered notifications**: New `EscalationWorker` in `daemon/server/core/escalation.go` implements 3-level escalation per priority. Critical PRs: reviewer (1h) → team (2h) → tech_lead (4h). Urgent PRs: reviewer (4h) → team (8h) → tech_lead (12h). Normal PRs: reviewer (24h) → team (48h). Priority determined by labels (`critical`, `security`, `breaking-change`, `hotfix`, `urgent`, `high-priority`) and file paths (`src/core/`, `src/security/`, `cmd/`). Escalation state persisted to `escalation_rules` bucket to prevent duplicate notifications.

- **Refactor**: Split `common/gitutil/git.go` (551 lines) into `git.go` (291 lines, high-level API), `git_ops.go` (125 lines, low-level git operations), `git_util.go` (104 lines, internal utilities). Split `daemon/handlers/webhook/webhook.go` into `webhook/` sub-package (previously done). Added tests for serial worker (4 tests), cross-space deps (4 tests), escalation (3 tests).

### Mid-Difficulty Features

- **Issue-PR bidirectional linking**: New `IssuePRLink` model and `issue_pr_links` bucket. Automatically extracts issue references (`Fixes #123`, `Closes org/repo#456`, `Resolves #N`) from PR titles and bodies during webhook processing and sync. Cross-repo references supported via `owner/repo#N` format. REST API: `GET /api/v1/repos/:rg/issues/:issue_id/prs`, `GET /api/v1/repos/:rg/prs/:pr_id/issues`, `POST /api/v1/repos/:rg/prs/:pr_id/sync-links`. PR body is now parsed from GitHub, GitLab, and Gitea webhooks and stored in `PRRecord.Body`.

- **PR templates & checklist validation**: New `PRTemplate` model and `pr_templates` bucket. Fetches PR templates from platform repos (`.github/PULL_REQUEST_TEMPLATE.md`, `.github/pull_request_template.md`, etc.) via new `GetFileContent` platform interface method. Checklist validation checks for unchecked items (`- [ ]`) in PR body, blocking merge until complete. REST API: `GET /api/v1/repos/:rg/template`, `POST /api/v1/repos/:rg/template/fetch`, `POST /api/v1/repos/:rg/prs/:pr_id/checklist`. New `GetPRBody` and `GetFileContent` methods added to all 5 platform clients.

- **Cross-repo PR dependency tracking**: New `PRDependency` model and `pr_dependencies` bucket. Parses `Depends-on: <url>` declarations from PR descriptions. When a PR is merged, downstream dependent PRs can be identified for rebase notification. REST API: `GET /api/v1/repos/:rg/prs/:pr_id/dependencies`, `GET /api/v1/repos/:rg/prs/:pr_id/dependents`, `POST /api/v1/repos/:rg/prs/:pr_id/sync-deps`.

- **Platform client interface extension**: Added `GetPRBody(ctx, owner, repo, number) (string, error)` and `GetFileContent(ctx, owner, repo, path) (string, error)` to `PlatformClient` interface. All 5 platform clients (GitHub, GitLab, Gitea, Bitbucket, Gerrit) implement both methods. Mock client updated accordingly.

### Low-Difficulty Features

- **Notification digest/batching**: Notifications for the same PR within a 5-minute window are now batched into a single digest message. The first event sends immediately; subsequent events are buffered and dispatched as a summary when the window expires. Reduces notification spam when a PR has multiple rapid events (e.g., approve + CI pass + label). Digest format: `📋 PR {id}: N events` with per-event-type counts.

- **Bottleneck identification**: New `GET /api/v1/stats/bottlenecks` endpoint identifies four categories of bottleneck PRs: reopened PRs (multiple reopen cycles), long-review PRs (open >48h with review activity), stale PRs (open >50% of review period with review requests), and frequent-reject PRs (≥2 review rejections). Also computes P90/P95 lead time percentiles. New `BottleneckStats` and `BottleneckPR` models.

- **Temporary privilege escalation tokens**: New `POST /api/v1/auth/temp-token` endpoint generates short-lived JWT tokens (1m–24h) with elevated permissions. Users with existing access can create temp tokens for CI/CD or one-off operations without sharing their main token. Temp tokens include `temp: true` claim and a `permissions` map checked by `RequirePermission` middleware before falling through to DB permissions. `GenerateTempToken`, `IsTempToken`, and `GetTempPermissions` APIs in `common/auth`.

- **Scheduled merge**: New `POST /api/v1/repos/:rg/prs/:id/schedule-merge` endpoint allows queuing a PR with a future merge time (RFC3339 format). The queue checker skips items whose `ScheduleAt` time has not yet arrived. `QueueItem` model now includes `ScheduleAt` field. `AddToQueueScheduled` exported from `queue.Manager` and `pr` sub-package.

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

## v20260514DEV > Unreleased

- **Bug fix**: Webhook idempotency — duplicate webhooks from platforms (e.g. GitHub retries) were processed multiple times. Added `webhook_dedup` bucket and delivery ID extraction (`X-GitHub-Delivery`, `X-Gitlab-Event-ID`, etc.) for dedup check.

- **Bug fix**: Webhook request body had no size limit. Added 1MB max via `http.MaxBytesReader` to prevent memory exhaustion attacks.

- **Bug fix**: `BatchClosePR` did not update local PR state in DB (only called platform API + audit log). Added `db.PutPRWithIndex` to persist `State: "closed"` like single `ClosePR`.

- **Bug fix**: `Consumer.Stop()` double-close panic on restart — `close(c.stop)` without nil check. Added nil guard and set to nil after close.

- **Bug fix**: `Manager/Poller/SerialWorker/SpamDetector.Stop()` double-close panic. Added nil guard and set to nil after close.

- **Bug fix**: `StartWebhookRetryWorker()` leaked goroutine on restart — old worker never stopped. Now calls `StopWebhookRetryWorker()` before creating new one.

- **Bug fix**: `writerActor` had no panic recovery — a single `db.PutPRWithIndex` panic would kill the writer goroutine permanently. Added `recover()` in both the main loop and per-request handler.

- **Bug fix**: `crypto.init()` silently disabled encryption when `ASIKA_MASTER_KEY` was unset. Now logs `slog.Warn` to alert operators that tokens are stored in plaintext.

- **Bug fix**: `FindPRByID` fallback scan returned early on `json.Unmarshal` errors (corrupted entries skipped remaining PRs). Changed to `return nil` to continue scanning.

- **Performance**: `getPRFromDB` fallback scan used `ForEach` (full table scan). Changed to `BucketForEachPrefix` scoped to repo group, reducing from O(n) to O(k).

- **Security**: GitHub/Bitbucket `VerifyWebhookSignature` — removed `strings.HasPrefix` branch before `hmac.Equal`, eliminating potential timing side-channel. Now compares full `sha256=hex` format in constant time.

- **Bug fix**: `mustMarshal` returned nil on json.Marshal failure, silently skipping dedup entry writes — caused duplicate notifications. Now writes a minimal fallback JSON (`{"_id":"..."}`) to preserve dedup protection.

- **Bug fix**: `cachedNotificationPrefs` held `sync.Mutex` during DB query, blocking all notification senders on cache refresh. Changed to `sync.RWMutex` with read-lock check / unlock-query-write-lock-update pattern.

- **Bug fix**: `ensureIndexes` (MongoDB) swallowed all index creation errors — startup never detected partial index failure. Now aggregates errors via `errors.Join` and returns combined error if any index fails.

- **Refactor**: `AppendAuditLogEx` (MongoDB) removed unnecessary `bson.Marshal` → `bson.Unmarshal` round-trip (3 serializations per write). Now constructs `bson.M` directly from entry fields. Extracted `writeAuditIndex` helper to eliminate 5 duplicate index-write blocks.

- **Bug fix**: `handlePRLabeled` ignored `event.Payload` — stale manager's "stale" label was not added to `PR.Labels`. Now extracts string payload as label name and deduplicates before appending.

- **Bug fix**: `recordSync` silently swallowed json.Marshal and DB errors — sync history silently lost. Changed signature to return error; callers now log with `slog.Error` without interrupting sync flow.

- **Bug fix**: `UpdateNotificationPrefs` did not invalidate the notifier preferences cache — changes took up to 30s to take effect. Now calls `ResetNotifierPrefsCache()` on successful write.

- **Bug fix**: `handlePRLabeled` ignored `event.Payload` — stale manager's string payload (e.g. "stale") was never added to `PR.Labels`. Now extracts string payload as label name with dedup.

- **Note**: Reverse index buckets (`issue_pr_links_by_pr`, `pr_dependents`) only populate for new writes after deployment. Existing data requires a one-time backfill: iterate `issue_pr_links` and `pr_dependencies` buckets to populate reverse indexes.

- **Bug fix**: Legacy `webhook_retries` records without `DeliveryID` (from pre-deployment) were never cleaned up. `StartWebhookRetryWorker` now scans and removes empty-`DeliveryID` records on startup.

- **Bug fix**: `FindPRByID` fallback scan silently skipped corrupted entries. Now logs `slog.Warn` with count of skipped corrupted entries for observability.

- **Bug fix**: GitHub webhook verification previously dropped SHA1 fallback (`X-Hub-Signature`), breaking legacy clients. Restored SHA1 fallback with constant-time `hmac.Equal` comparison alongside SHA256.

- **Bug fix**: `writerActor` main-loop panic killed the writer goroutine permanently — subsequent writes blocked forever. Now auto-restarts up to 3 times on panic.

- **Performance**: `dedupMu` lock in `WebhookHandler` now covers both dedup check and mark-as-processed atomically, preventing concurrent duplicate processing of the same delivery.
