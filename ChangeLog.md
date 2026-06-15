# ChangeLog for Asika

## v20260615DEV

- **Fix**: Block draft PRs from being merged by the queue. `MarkDraft` now removes the PR from the merge queue, and `ShouldMerge` short-circuits on `IsDraft` as a defensive gate. Previously, marking an already-queued PR as draft still let the queue merge it once approvals/CI passed — violating the draft PR contract.
- **Fix**: Stabilise queue out-order. `CheckQueue` now sorts same-priority items by `AddedAt` (FIFO), matching the order exposed by `GetQueueItems` to the UI. Previously the actual merge order did not match what users saw.
- **Fix**: Batch merge now honours configured merge strategy. `batch_merge` validates the `method` argument against `{merge,squash,rebase}` and falls back to `queue.SelectMergeMethod` (rules → default → platform default) when `method` is empty. Also skips draft PRs. Previously batch merge hardcoded `"merge"`, bypassing `MergeStrategyRules`/`DefaultMergeMethod`/`FastForwardOnly` rebase.
- **Security**: Move `trigger-ci` endpoint from `prsExtra` (viewer-writable) to `prsMerge` group (`RequirePermission("merge")`). Only operator/admin can trigger CI now; viewers could previously call the endpoint.
- **Security**: Scope Gerrit Change-ID lookup to the caller's repo group. Endpoint moved from `protected.GET /gerrit/change/:change_id` (auth-only, full-table scan) to `protected.GET /repos/:repo_group/gerrit/change/:change_id` (with `RequireRepoGroupAccess`/`RequireRepoAccess`/`RequireSpaceAccess`), and the handler now uses `BucketForEachPrefix` so PRs in other tenant groups are never inspected or returned.
- **Fix**: Escape Mermaid node labels in the dependency-graph endpoint. PR titles and PR IDs are now sanitised (quotes/backslash/newline escaped, ID character set restricted) to prevent Mermaid source injection from external PR data.
- **Fix**: `asika queue priority` CLI now parses the priority argument with `strconv.Atoi` and validates the 0–100 range before encoding the body with `json.Marshal`. Previously the raw CLI argument was string-substituted into a JSON template.
- **Fix**: `auto-label` returns HTTP 500 when the platform client is nil instead of falsely reporting `"labels applied"`. Response now includes `applied` and `failed` counts.
- **Fix**: Tighten `webhook.filter.matchBranchPattern` to escape regex metacharacters before substituting `*` → `.*`. Patterns containing `(`, `)`, `+`, `?`, `[`, etc. now match their literal form instead of silently failing to compile.
