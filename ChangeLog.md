# ChangeLog for Asika

## v20250517DEV > v20260523DEV

### Security Fixes

- **Security**: `RequireRepoAccess()` middleware was defined but never mounted. Now mounted on PR, queue, template, dependency, and issue link routes.
- **Security**: Non-admin API keys were rejected in `RequireRepoGroupAccess()` because `apikey:<name>` doesn't exist in user DB. Now uses key scope directly for API key auth.
- **Security**: Cookie JWT had no CSRF protection. `CSRFProtect()` and `IssueCSRFToken()` now mounted on protected routes, with Bearer/API key auth exempted.
- **Security**: JWT session only checked existence, not `ExpiresAt`. Now validates expiry and deletes expired sessions.
- **Security**: Deleting user or changing role didn't revoke existing JWT sessions. Now deletes sessions, API keys, and OIDC links.
- **Security**: Temp tokens had no `sid`/`jti` and couldn't be revoked. Now stored as sessions with jti claim.
- **Security**: TOTP backup codes returned to user didn't match stored hashes. Now generates once, hashes those, returns same plain codes.
- **Security**: TOTP backup codes were reusable. Now consumed after single use.
- **Security**: TOTP secret leaked to third-party QR service. Now generates QR locally in browser.
- **Security**: OIDC `oidcStates` global map had no concurrency protection. Now uses `sync.Mutex`.
- **Security**: OIDC link API allowed forging any provider/subject. Removed; links only created in verified callback flow.
- **Security**: OIDC auto-create could overwrite same-name local users. Now checks for existing local user and returns 409.
- **Security**: OIDC endpoint config logic was reversed. Now sets endpoints when AuthURL+TokenURL are provided.
- **Security**: Login page couldn't safely show OIDC providers. Added public `/api/v1/auth/oidc/providers` endpoint.
- **Security**: Global `/logs`, `/stats`, `/reports`, `/stacks`, `/spaces` lacked tenant filtering. Now filter by user's repo_group/space scope.
- **Security**: Public feed leaked all repo_group activity. Now requires repo_group parameter for unauthenticated access.
- **Security**: PR dependencies and issue links queries not bound to repo_group. Now verify PR's repo_group matches route parameter.
- **Security**: Cross-space dependency routes didn't verify caller can access both PRs' spaces. Now checks both.
- **Security**: JWT stored in localStorage (XSS-visible) alongside HttpOnly cookie. Removed localStorage token handling; cookie-only auth.
- **Security**: Notification settings page derived username from pathname and tried to read HttpOnly cookie. Now uses server-injected username.
- **Security**: Frontend used `innerHTML` with unescaped error messages. Now uses `textContent` and proper escaping.
- **Security**: Feishu bot built JSON with `fmt.Sprintf` and used `http.DefaultClient` without timeout. Now uses `json.Marshal` and 30s timeout client.
- **Security**: Bot `/adduser` commands transmitted plaintext passwords in chat. Now generates random passwords.
- **Security**: Initial config didn't validate hookpath for absolute path and `..`. Now validates all hook paths.
- **Security**: `DecryptSecretsInConfig` silently kept `enc:` ciphertext when master key was missing. Now returns error.
- **Security**: `CreateUser` could overwrite existing users. Now checks existence and returns 409.
- **Security**: CORS allowed `*` origin with `Authorization` header. Now restricts wildcard to `Content-Type` only; explicit origins get credentialed headers.

### Bug Fixes

- **Bug fix**: PR ID index written as `repoGroup:prID` but queried with bare `prID`. `FindPRByID` now requires repo_group parameter.
- **Bug fix**: `merge()` used global PR ID lookup without repo_group. Now passes `item.RepoGroup`.
- **Bug fix**: Queue recovery used global PR ID to check merge status. Now uses group-scoped lookup.
- **Bug fix**: Serial worker marked items `Status: "ready"` but `CheckQueue` only processed `waiting`/`checking`/`failed`. Now also processes `ready`.
- **Bug fix**: `IsReadyToMerge` logged DEBUG at `slog.Error` level. Now uses `slog.Debug`.
- **Bug fix**: Write-permission check failure kept the approver (fail open). Now skips approver (fail closed).
- **Bug fix**: GitHub `triage` permission counted as write. Now only `push`/`maintain`/`admin`.
- **Bug fix**: Bitbucket `HasWritePermission` always returned `true`. Now implements proper API check.
- **Bug fix**: Gerrit `HasWritePermission` only checked account existence. Now checks submit permission via access API.
- **Bug fix**: Gitea webhook didn't accept `sha256=` prefixed signatures. Now strips prefix before comparison.
- **Bug fix**: GitHub `RevertPR` hardcoded `https://api.github.com`. Now uses client's base URL.
- **Bug fix**: GitLab `RevertPR` constructed empty base URL. Now defaults to `https://gitlab.com/api/v4/`.
- **Bug fix**: Gitea/Forgejo `RevertPR` only created PR without revert branch. Now returns explicit unsupported error.
- **Bug fix**: Bitbucket `ListPRs` only read first page. Now paginates using `next` field.
- **Bug fix**: Gerrit `ListPRs` fixed at 100 results. Now paginates with max 500.
- **Bug fix**: Bitbucket/Gerrit `GetFileContent` used `url.PathEscape` which encoded `/`. Now escapes path segments individually.
- **Bug fix**: Gerrit `GetFileContent` didn't include branch/revision. Now adds `?ref=HEAD`.
- **Bug fix**: Persistent clone created bare repo but `syncDefaultBranch` needed worktree. Now uses non-bare clone.
- **Bug fix**: Branch delete sync returned after first successful target. Now processes all targets.
- **Bug fix**: Sync PR state used title fallback to match target PR. Removed title matching; only branch/SHA.
- **Bug fix**: Target PR merge failure auto-closed the PR. Now only logs error and notifies.
- **Bug fix**: Git `Push` defaulted to `Force: true`. Now defaults to `false`; sync operations use explicit force.
- **Bug fix**: Cherry-pick ignored deleted files. Now handles delete/rename/mode changes.
- **Bug fix**: Cherry-pick target branch not validated. Now validates refname.
- **Bug fix**: Mongo PR collection had global unique index on `id`. Now compound index on `(repo_group, platform, id)`.
- **Bug fix**: Mongo `Session`/`OIDCLink` structs lacked `bson` tags. Now added.
- **Bug fix**: Mongo `ForEach`/`BucketForEachPrefix` returned wrapped BSON documents. Now extracts `data` field like bbolt.
- **Bug fix**: Webhook dedup check and mark were not atomic. Now holds lock through both operations.
- **Bug fix**: Webhook retry success didn't write dedup/health records. Now writes both.
- **Bug fix**: Webhook dedup records had no TTL cleanup. Now runs hourly cleanup of records older than 24h.
- **Bug fix**: Poller overwrote platform timestamps with `time.Now()`. Now preserves platform values.
- **Bug fix**: Consumer `handlePROpened` overwrote creation time. Now only sets if zero.
- **Bug fix**: Event bus `Subscribe`/`Publish` panicked if `Init()` not called. Now auto-initializes.
- **Bug fix**: Backup restore could leave DB closed on failure. Now validates before closing.

### Performance & Stability

- **Performance**: PR queries and issue/dependency sync used full table scans. Now use indexed lookups.
- **Performance**: Poller rewrote unchanged PRs every round. Now skips writes for unchanged PRs.
- **Performance**: Logs/stats/reports scanned full buckets without limits. Now enforce max scan limits.
- **Stability**: Bot HTTP calls used `http.DefaultClient` without timeout. Now use 30s timeout clients.
- **Stability**: Report scheduler HTTP calls had no timeout. Now uses 30s timeout.
- **Stability**: Background goroutines had no shutdown path. Now use proper stop channels.
- **Stability**: `VerifyNotifiers` read global notifiers without lock. Now uses `RLock`.
- **Bug fix**: Config encryption encrypted empty platform tokens. Now skips empty values.
- **Bug fix**: JWT secret only checked non-empty, not weak values. Now rejects known defaults and requires 16+ chars.
- **Bug fix**: `GetRepoGroups` missed some `RepoGroup` fields vs `GetRepoGroupByName`. Now uses shared normalize function.
- **Bug fix**: Queue status strings scattered as literals. Now defined as constants.
- **Bug fix**: `ParseIssueLinks` didn't support `refs #123`. Now includes `refs?` in regex.
- **Bug fix**: Mongo prefix queries used regex. Now use range queries.
- **Bug fix**: Self-update download had no size limit. Now uses `LimitedReader`.
- **Hardening**: `rand.Read` errors now checked in OIDC state, TOTP secret, and backup code generation.
- **Hardening**: Webhook body size limit now configurable (default 1MiB).
- **Hardening**: `ListBackups` now filters to `.db` files only.
- **Hardening**: Locale now request-scoped instead of global state.
- **Hardening**: `ListAPIKeys`/`ListUsers` now support pagination.
- **Hardening**: Stats/reports period capped at 365 days.
- **Hardening**: Discord bot `Stop()` now uses `sync.Once` to prevent double-close.
- **Tests**: Added route permission matrix, storage conformance, webhook dedup concurrent, and platform client contract tests.


### Features

- **Feature**: Label-based PR subscription notifications. Users can now subscribe to specific PR labels via `LabelSubs` in their notification preferences. When a PR is labeled (manually, via auto-labeler, or via webhook), only users subscribed to matching labels receive notifications. Supports glob patterns (e.g. `"area/*"` matches `"area/frontend"`). Empty/nil `LabelSubs` subscribes to all labels (backward compatible). The labeler publishes `pr_labeled` events after auto-labeling, and the consumer's handlePRLabeled triggers label-aware notifications via `SendNotificationWithLabels`.
- **Feature**: Notification digest mode. `DigestMode` in user preferences now works — `"hourly"` and `"daily"` modes buffer notifications and send them as a summary at the configured interval. `"realtime"` (default) sends immediately as before.
- **Feature**: PR comment @mention notifications. Comments containing `@username` now extract mentions and trigger targeted notifications to mentioned users via `NotifyMentions`.
- **Feature**: Auto-merge rules. New `[auto_merge]` config section with named rules that automatically merge PRs when conditions are met (labels, required approvals, CI status). Evaluated on `pr_approved` events and via a periodic 5-minute scanner.
- **Feature**: Queue priority sorting. `QueueItem.Priority` field (already in model) is now used — items are sorted by priority (higher first) before processing in `CheckQueue`.
- **Feature**: Stale exclude authors. New `exclude_authors` field in `[stale]` config prevents specific authors (e.g. bots) from being marked stale.
- **Feature**: Bitbucket full webhook support. Bitbucket webhooks now parse PR created/merged/closed/declined events in addition to comment events.

### Security Fixes

- **Security**: Telegram/Slack/Discord bots returned `admin=true` for any user when allowlists were empty. Now defaults to reject-all with warning log, consistent with Feishu.
- **Security**: Feishu event handler skipped token verification on malformed JSON. Now returns 400 on parse failure.
- **Security**: API key HMAC used hardcoded secret `"asika-apikey-hmac-v1"`. Now uses JWT secret from config.
- **Security**: Config hot-reload wrote unencrypted tokens to disk. Now uses `SaveToFile` with encryption.
- **Security**: Config rollback wrote masked secrets (`"***"`) over real secrets. Now preserves secrets from current config.
- **Security**: `SaveToFile` temp file used default umask permissions. Now explicitly sets 0600.
- **Security**: `RequireSpaceAccess()` failed open on DB errors. Now returns 500.
- **Security**: `RequireRepoAccess()` failed open when repo could not be resolved. Now returns 403.
- **Security**: API key scope and user scope combination was undefined. Now intersects both scopes.
- **Security**: `SetLocale` cookie lacked `Secure` flag. Now sets `Secure=true`.
- **Security (XSS)**: `team.html` used `innerHTML` with user data. Now uses DOM API with `textContent`.
- **Security (XSS)**: `apikeys.html` used HTML escaping in JS string context. Added `escapeJs()` function.
- **Security (XSS)**: `pr_list.html` inserted `body.error` into `innerHTML` without escaping and didn't validate `html_url` scheme. Fixed both.
- **Security (XSS)**: `pr_detail.html` `D(m)` function used `innerHTML` with error message. Now uses `textContent`.

### Bug Fixes

- **Bug fix**: `db.Get` returned `nil, nil` for missing keys, causing callers to misinterpret as 500. Now returns `ErrNotFound`.
- **Bug fix**: PR ID global index (`pr_index_by_id`) used bare `prID` as key, causing cross-platform collisions. Now uses `repoGroup:prID`.
- **Bug fix**: Queue checker, rebase handler, and sync handler used global PR ID without repo_group binding. Now pass repo_group for scoped lookups.
- **Bug fix**: Mongo `Get` returned wrapped `{"_id":..., "data":...}` document instead of raw value. Now extracts `data` field.
- **Bug fix**: Mongo `Put`/`Get` semantic mismatch broke config version, label rules, and other raw-value buckets. Fixed by aligning Put wrapper with Get extraction.
- **Bug fix**: Config snapshot timestamp always returned `time.Now()`. Now persists `created_at` in snapshot.
- **Bug fix**: `PutPRWithIndex` silently ignored index write errors. Now checks and returns errors.
- **Bug fix**: `GetPRByIndex` fallback scan didn't filter by repo_group. Now enforces repo_group constraint.
- **Bug fix**: `GetPRByIndex` fallback only repaired ID index, not RG index. Now repairs both.
- **Bug fix**: Gerrit `GetOwnerRepoFromGroup` split single-segment project into `("", project)`, producing empty owner. Now returns `(project, "")` for Gerrit.
- **Bug fix**: Gerrit `GetCloneURL` and `GetToken` had no gerrit case. Now returns proper URL and password.
- **Bug fix**: Bitbucket `GetApprovals` deleted all reviewers from approver set, including approved ones. Now only counts approved participants.
- **Bug fix**: Bitbucket `RequestReview` called `RequestChanges` (wrong API). Now uses `Update`.
- **Bug fix**: Bitbucket `GetPRBody`/`GetFileContent` didn't check HTTP status codes. Now returns error on non-2xx.
- **Bug fix**: Gerrit `GetFileContent` didn't handle XSSI prefix or base64 encoding. Now strips prefix and decodes.
- **Bug fix**: `merge_checker.go` called `os.Exit(1)` on check failure. Now logs error and continues.
- **Bug fix**: Syncer `acquireLock` failed open on DB error. Now returns false (fail closed).
- **Bug fix**: Syncer `AddRemote` didn't update URL when remote already existed. Now checks and updates.
- **Bug fix**: `RebaseAndPush` didn't detect missing base commit. Now returns explicit error.
- **Bug fix**: Hook runner used `time.AfterFunc` + `Process.Kill()` (nil-panic risk). Now uses `exec.CommandContext`.
- **Bug fix**: Hook runner swallowed hook failures. Now returns structured errors.
- **Bug fix**: Hook path validation only on hot-reload, not initial config. Added `ValidateHookPath()`.
- **Bug fix**: `RestoreBackup` closed DB before validating backup, leaving service broken on failure. Now validates first, then atomically replaces.
- **Bug fix**: `RestoreBackup` didn't check DB type. Now rejects non-bbolt databases.
- **Bug fix**: `downloadWithProgress` ignored `f.Write` errors and had no size limit. Now checks errors and limits to 100MiB.
- **Bug fix**: Self-update SSE used hand-written JSON with unescaped error messages. Now uses `json.Marshal`.
- **Bug fix**: Self-update binary replacement wasn't atomic. Now writes to temp file, fsyncs, then renames.
- **Bug fix**: `verifyWebChecksum` read entire binary into memory. Now uses streaming hash.
- **Bug fix**: Poller used `context.Background()` with no timeout. Now uses 60s timeout.
- **Bug fix**: Self-update GitHub API calls used `context.Background()`. Now uses 30s timeout.
- **Bug fix**: Failed queue items retried indefinitely with no backoff. Now tracks `RetryCount`/`NextRetryAt` with max 5 retries.
- **Bug fix**: Merge expression could bypass CI/conflict hard gates. Now conflicts always block; CI requires `AllowExpressionOverrideCI`.
- **Bug fix**: Serial worker `markMergeable` only updated `serial_queue`, not main queue. Now updates both.
- **Bug fix**: Cross-space cache invalidated on space CRUD. Now properly calls `invalidateSpaceCache()`.
- **Bug fix**: `ListPRs` by ID could cross repo_group. Now enforces repo_group constraint.
- **Bug fix**: API key `last_used_at` update error silently ignored. Now logs warning.
- **Bug fix**: `merge_checker.go` imported unused `os` package. Removed.
- **Bug fix**: `RequireRepoGroupAccess` with API key used key scope exclusively, ignoring user scope. Now intersects both.

### Features

- **Feature**: Added `CanComment`/`CanLabel` permissions to API key creation.
- **Feature**: Added `/rules/labels` API route for label rules management.
- **Feature**: Added `RequireSelfOrAdmin()` middleware for user-scoped routes.
- **Feature**: Added `AllowExpressionOverrideCI` config option for merge queue.
- **Feature**: Added `RetryCount`/`NextRetryAt` fields to QueueItem for backoff support.
- **Feature**: Added `publicFeed` field to Feed struct and `GetConfig()` response.
- **Feature**: Moved `/feed.xml` to unprotected route with handler-level auth check for `PublicFeed`.

### Refactoring

- **Refactor**: Added `ErrNotFound` sentinel error to `db` package for consistent missing-key handling.
- **Refactor**: Updated `pr_index_by_id` bucket key format from `{prID}` to `{repoGroup}:{prID}`.
- **Refactor**: Config snapshots now store `{config, created_at}` wrapper for accurate timestamps.
- **Refactor**: Bot `isAdmin()` in Telegram/Slack/Discord now defaults to reject-all (was allow-all).

### UX Improvements

- **Feat**: Rewrote `asika wizard` with grouped selection, visible options, and config summary. Platforms and notification channels now use numbered multi-select instead of prompting every option. Added server address confirmation, initialization status check, risk disclaimer, and pre-write summary review.
- **Feat**: Unified font-family across all pages (layout, login, wizard) with CJK font support. Added `/api/v1/i18n` endpoint for fetching translations as JSON. Implemented locale switching without page reload using `data-i18n` attributes and dynamic DOM replacement.

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
