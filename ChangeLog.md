# ChangeLog for Asika

## v20260517DEV > v20260617DEV

### UX Improvements

- **Feat**: Rewrote `asika wizard` with grouped selection, visible options, and config summary. Platforms and notification channels now use numbered multi-select instead of prompting every option. Added server address confirmation, initialization status check, risk disclaimer, and pre-write summary review.

### Bug Fixes & Stability

- **Fix**: GitHub/Bitbucket `GetCIStatus` swallowed API errors, returning `"none", nil` — PRs could merge without CI verification when the CI API was temporarily unavailable.
- **Fix**: `globalNotifiers` in `core/notifier.go` and `handlers/notifier.go` had data races during config hot-reload. Added `sync.RWMutex` protection.
- **Fix**: `os.Chmod` error ignored after self-update binary replacement, could brick the daemon.
- **Fix**: `json.Marshal`/`db.PutPRWithIndex` errors silently ignored in `rebase.go`, `close.go`, `queue/manager.go`, `queue/serial_worker.go`, and all platform bot commands — could corrupt PR records or lose state.
- **Fix**: Bitbucket/GitLab client constructors returned nil-client structs on auth failure, causing nil-pointer panics on all subsequent API calls.
- **Fix**: `Consumer.Start()`/`Stop()` had data races on multiple struct fields. Added `lifecycleMu` mutex. Debounce closures captured `c.workers` which became nil after Stop.
- **Fix**: `workerPool.UpdateConfig()` wrote config fields without synchronization, racing with `adjust()` goroutine.
- **Fix**: Spam auto-clean goroutine directly mutated shared `cfg` pointer fields, racing with spam detector reads.
- **Fix**: `verifyPRState` goroutine received a context that was immediately cancelled by parent's `defer cancel()`. Verification logic was effectively disabled.
- **Fix**: `Syncer.notifyFn`/`recordWriter` accessed without synchronization during config reload vs runtime reads.
- **Fix**: `TOCTOU` race in `Stop()` methods for queue Manager, SerialWorker, SpamDetector — double-close panic on concurrent calls. Replaced with `sync.Once`.
- **Fix**: Rate limiter `getVisitor()` had TOCTOU race on `sync.Map` — replaced `Load`+`Store` with `LoadOrStore`.
- **Fix**: `workerPool.Submit()` blocked indefinitely when task buffer was full. Now uses `select` with stop channel.
- **Fix**: Config reload failure returned HTTP 200 instead of 500.
- **Fix**: Migration functions (`MigrateRepoGroupNames`, `MigratePRStates`, `SyncPRStates`) silently ignored all db write errors.
- **Fix**: Queue recovery ignored `db.Delete` error for already-merged PRs.
- **Fix**: `spam.go` `sendSpamNotificationWithContext` shadowed the passed `ctx` with `context.Background()`, defeating timeout control.
- **Fix**: Sync retry goroutine used `context.Background()` with no timeout — could hang indefinitely.
- **Fix**: Consumer handler goroutines (`labeler`, `reviewer`, `syncPRLinks`, `NotifyCrossSpaceDeps`, `UpdateStackMemberStateOnMerge`) had no timeout — could accumulate on blocking calls.
- **Fix**: Background goroutines (stale checker, webhook health, token cleanup) used unreachable `defer ticker.Stop()` in `for range ticker.C` loops. Converted to `select` with stop channels.
- **Fix**: `serial_worker.updateItem` silently ignored marshal and db write errors — serial validation state machine could get permanently stuck.
- **Perf**: Middleware `resolveRepoFromRequest` did full PR bucket scan on every authenticated request with `pr_id`. Now uses index-first lookup.
- **Perf**: `findPRInDB` in `sync.go` did full scan. Now uses index-first lookup.
- **Perf**: `ParseDependencies` compiled regex on every call. Moved to package-level variable.

### Security Fixes

- **Security**: Feishu event endpoint (`/api/v1/feishu/event`) lacked verification token check, allowing forged events. Added `VerificationToken` validation against config + 1MB body size limit.

- **Security**: Feishu bot `isAdmin()` returned `true` for any user when no admin/operator/viewer IDs were configured. Now defaults to reject-all with a warning log.

- **Security**: Batch label endpoint (`POST /api/v1/repos/:rg/prs/batch/label`) had no permission check beyond viewer role. Added `RequirePermission("label")`.

- **Security**: Comment endpoint (`POST /api/v1/repos/:rg/prs/:id/comment`) had no permission check beyond viewer role. Added `RequirePermission("comment")`.

- **Security**: Issue-link, template fetch, and dependency sync routes lacked repo group authorization. Added `RequireRepoGroupAccess()` middleware.

- **Security**: Cross-space dependency endpoints (`GET/POST /api/v1/cross-space-deps/...`) lacked authorization. Added `RequirePermission("merge")` for write operations.

- **Security**: `AuthMiddleware` skipped all `/api/v1/auth/*` prefixed routes, preventing `RequireAuth()` from working on `temp-token` and `fingerprints` sub-routes. Now only skips `login` and `logout`.

- **Security**: Temp token creation allowed granting arbitrary permissions regardless of the user's actual role. Now validates each permission against role hierarchy (viewer cannot grant operator-level permissions).

- **Security**: Config API (`GET /api/v1/config`) only masked GitHub/GitLab/Gitea tokens and JWT secret. Now comprehensively masks Forgejo, Codeberg, Bitbucket, Gerrit credentials, webhook secret, notifier tokens, bot tokens, SMTP password, DB URI, Feishu secrets via unified `maskConfig()` function.

- **Security**: Config snapshots stored plaintext (decrypted) config in DB. Now masks all secrets via `maskConfigForStorage()` before persisting.

- **Security**: Encryption (`EncryptTokensInConfig`/`DecryptTokensInConfig`) only covered platform tokens. Extended to Gerrit password, JWT secret, fingerprint secret, webhook secret, Feishu app secret/encrypt key, Telegram/Discord/Slack tokens.

- **Security**: MongoDB prefix queries (`ForEachPrefix`, `BucketForEachPrefix`) used unsanitized user input in `$regex`, enabling regex injection. Added `regexp.QuoteMeta()` escaping.

- **Security**: Login cookie had `Secure=false` and no SameSite. Set `Secure=true` and `SameSite=Lax`. Added CSRF protection middleware with token generation/validation.

- **Security**: pprof debug endpoints (`/debug/pprof/*`) were unauthenticated. Added `RequireAuth()` + `RequireRole("admin")`.

- **Security**: Notification preference endpoints (`GET/PUT /api/v1/users/:username/notifications`) allowed any authenticated user to read/modify any user's preferences. Added ownership check (self or admin only).

- **Security**: SSE event stream (`GET /api/v1/events`) broadcast all events to all authenticated users without filtering. Added per-user repo group access filtering.

- **Security**: `GetRepoGroupByName` silently fell back to "default" group for unknown repo groups. Now returns nil for unknown groups.

- **Security**: Webhook dedup key used only `deliveryID`, allowing cross-platform collisions. Now uses `platform:repoGroup:deliveryID` composite key.

- **Security**: Webhook was marked as processed before handling, causing lost events if processing failed. Now marks processed only after successful handling.

- **Security**: RestoreBackup used string prefix check for path traversal, bypassable via paths like `/path/backups_evil/x`. Now uses `filepath.Rel` and rejects `..` components.

- **Security**: API key list endpoint returned `KeyHMAC` field unnecessarily. Removed from response struct.

- **Security**: API key creation permissions struct was missing `CanRevert` field. Added it.

- **Security**: `FetchPRTemplate` passed `nil` context to platform client. Now uses `context.Background()`.

- **Security (XSS)**: Audit log page (`audit.html`) used `innerHTML` to render log entries containing user-controlled data. Now uses DOM API with `textContent`.

- **Security (XSS)**: Reports page (`reports.html`) used `innerHTML` to render report content. Now uses DOM API with `textContent`.

- **Security (XSS)**: Users page (`users.html`) used inline `onclick` with `escapeHtml()` (HTML-only escaping) for JS string parameters. Replaced with `data-*` attributes + `addEventListener`.

- **Security (XSS)**: Spaces page (`spaces.html`) used `innerHTML` for space name/description/created_by. Now uses DOM API + `escapeHtml`.

- **Security (XSS)**: Error messages in `users.html`, `apikeys.html`, `queue.html` used `innerHTML` with server error messages. Now uses `textContent`.

### Bug Fixes

- **Bug fix**: `CherryPick()` used `HardReset` to replace the entire target tree with the source commit tree, destroying base branch content. Replaced with proper diff-based cherry-pick that applies only the changes introduced by the source commit.

- **Bug fix**: Git clone operations used a single shared directory (`cfg.Git.RepoClonePath`), risking cross-repo interference. Now uses per-repo scoped paths based on URL hash.

- **Bug fix**: Sync records were written to `BucketPRs` via `PutPRWithIndex` instead of `BucketSyncHistory`. Added dedicated `writeSyncRecord()` method on writer actor.

- **Bug fix**: `CheckQueue()` had no concurrency protection, allowing duplicate merges from concurrent triggers. Added `sync.Mutex` around the entire check cycle.

- **Bug fix**: Self-update used GET method (triggerable via cross-site navigation). Changed to POST.

- **Bug fix**: Self-update skipped checksum verification when checksum asset was missing, and ignored `io.Copy` errors. Now requires checksum and checks all write errors.

- **Bug fix**: `MergeCommitSHA[:8]` in rebase/cherry-pick logs panicked if SHA was shorter than 8 characters. Added `shortSHA()` helper with length check.

- **Bug fix**: Scheduled report URL construction used `"http://localhost" + addr`, producing invalid URLs for non-standard bind addresses (e.g., `0.0.0.0:8080`). Now parses host correctly.

- **Bug fix**: Rate limiter `lastSeen` was written without synchronization, causing data race. Added per-visitor `sync.Mutex`.

- **Bug fix**: Consumer `Stop()` did not unsubscribe from the event bus, leaking channels. Added `events.Unsubscribe()` call.

- **Bug fix**: i18n `SetLocale()` set global state, causing concurrent requests to overwrite each other's locale. Changed to request-scoped locale via gin context.

### Performance

- **Performance**: Labeler `compiledPatterns` map grew without bound. Added 1000-entry LRU eviction.

- **Performance**: Rate limiter cleanup goroutine was started on every `RateLimit()` middleware construction. Changed to `sync.Once` singleton.

### Refactoring

- **Refactor**: Split `daemon/server/middleware.go` (705 lines) into `middleware.go` (543 lines, auth/permission middleware), `middleware_helper.go` (128 lines, extractToken/resolveRepoFromRequest helpers), `middleware_csrf.go` (88 lines, CSRF protection).

- **Refactor**: Split `common/config/config.go` (682 lines) into `config.go` (512 lines, Load/SaveToFile/validate), `config_snapshot.go` (151 lines, SaveConfigSnapshot/RollbackConfig/ListConfigVersions), `config_mask.go` (83 lines, maskConfig/maskToken/maskSecret).

### Features

- **Settings UI**: Expanded HTML settings page with comprehensive configuration options. Added collapsible sections for Server, Events/Webhook, Updates, Worker Pool, Quiet Hours, Feed, Reports, and Close Reasons. Enhanced Spam Detection with similar title trigger and threshold. Enhanced Stale PR with custom comment templates. All new settings support hot-reload via config API.

- **CLI Interactive Review**: New `asika pr review <group> <id>` command for interactive PR review with diff viewing and inline comments. Supports file navigation, colored diff output, and line-specific comments.

- **Webhook Event Filter**: New `[webhook_filter]` configuration section to ignore specific events, authors, or labels. Reduces notification noise from bots and dependency updates.

- **Label-based Notification Routing**: New `[notify_rules]` configuration section to route notifications to specific channels based on PR labels. Supports priority-based notification for critical/security PRs.

- **Auto-Rebase Worker**: New `[auto_rebase]` configuration section to automatically rebase open PRs with conflicts. Supports excluding specific labels and authors.

- **Audit Log Export CLI**: New `asika logs list` and `asika logs export` commands for listing and exporting audit logs in JSON/CSV format with various filters.

- **Enhanced PR List API**: Added `search`, `has_conflict`, `sort_by`, `order` query parameters to `GET /api/v1/repos/:repo_group/prs` endpoint.

- **Webhook Configuration API**: New `GET/POST/DELETE /api/v1/webhooks` endpoints for managing webhook configurations via API.

- **Batch Rebase API**: New `POST /api/v1/repos/:repo_group/prs/batch/rebase` endpoint for batch rebase operations.

- **PR Diff API**: New `GET /api/v1/repos/:repo_group/prs/:pr_id/diff` endpoint for retrieving PR diff content.

- **Inline Comment API**: New `POST /api/v1/repos/:repo_group/prs/:pr_id/comment-line` endpoint for posting inline comments on specific lines.

