# Asika Enhancement Implementation Plan

## Overview
This document tracks the implementation of 14 major enhancements to Asika.

## Status Legend
- ⏳ Pending
- 🚧 In Progress
- ✅ Completed

---

## Phase 1: Queue & Batch Operations (High Priority)

### #2: Merge Queue Priority Scheduling ⏳
**Files to modify:**
- `common/models/models.go` - QueueItem.Priority already exists ✅
- `daemon/queue/manager.go` - Sort by priority in GetQueue
- `daemon/handlers/queue.go` - Add SetPriority API
- `daemon/server/routes.go` - Route for priority update
- `daemon/platform/*/commands_pr.go` - Bot commands for priority
- `lib/commands/queue.go` - CLI command for priority

**Implementation:**
1. Add priority-based sorting to queue listing
2. Add API endpoint: `PUT /api/v1/repos/:rg/queue/:pr_id/priority`
3. Add bot commands: `/priority <group> <pr_id> <high|medium|low>`
4. Add CLI: `asika queue priority <group> <pr_id> <1-100>`
5. Add "Fast Track" label detection (priority/critical → priority=100)

---

### #3: Batch Operations Enhancement ⏳
**Files to create/modify:**
- `daemon/handlers/pr/batch_merge.go` - New file
- `daemon/handlers/pr/batch_cherrypick.go` - New file
- `daemon/handlers/prs.go` - Export batch handlers
- `daemon/server/routes.go` - Batch merge/cherrypick routes
- `lib/commands/pr.go` - CLI batch commands
- Frontend files (WebUI multi-select) - TBD

**Implementation:**
1. Batch Merge: POST /api/v1/repos/:rg/prs/batch-merge with {pr_ids: [], method: ""}
2. Batch Cherry-pick: POST /api/v1/repos/:rg/prs/batch-cherrypick with {pr_ids: [], target_branch: ""}
3. CLI: `asika pr batch-merge <group> <id1,id2,...> --method squash`
4. CLI: `asika pr batch-cherrypick <group> <id1,id2,...> --branch <target>`
5. WebUI: Add checkbox column to PR list table

---

## Phase 2: PR Templates & Workflow (High Priority)

### #4: PR Template and Checklist Enhancement ⏳
**Files to modify:**
- `daemon/handlers/pr_templates.go` - Add ChecklistProgress API
- `daemon/server/routes.go` - Route for checklist status
- `daemon/platform/*/commands_pr.go` - Bot reminder command
- Frontend templates - Show checklist progress bar

**Implementation:**
1. Parse checklist from PR body: `- [ ] item` and `- [x] item`
2. Add API: GET /api/v1/repos/:rg/prs/:id/checklist → {total: 5, completed: 3, items: [...]}
3. Bot command: `/checklist <group> <pr_id>` shows progress
4. WebUI: Show progress bar on PR detail page
5. Block merge when checklist incomplete (based on config)

---

### #9: Draft PR Workflow Enhancement ⏳
**Files to modify:**
- `daemon/handlers/pr_extra.go` - Add SkipCI logic
- `daemon/consumer/consumer.go` - Skip CI trigger for drafts
- `daemon/platform/*/commands_pr.go` - Add /draft and /ready commands
- `lib/commands/pr.go` - Add CLI draft/ready commands

**Implementation:**
1. Detect draft state from platform API
2. Skip CI webhook trigger when PR is draft
3. Auto-trigger CI when marked ready (call platform CI API)
4. Bot: `/draft <group> <pr_id>` and `/ready <group> <pr_id>`
5. CLI: `asika pr draft <group> <id>` and `asika pr ready <group> <id>`

---

## Phase 3: Platform Enhancements (High Priority)

### #5: Gerrit Change-Id Bidirectional Mapping ⏳
**Files to modify:**
- `common/models/models.go` - Add PRRecord.GerritChangeID field
- `common/db/db.go` - Add gerrit_change_id_index bucket
- `common/platforms/gerrit.go` - Store Change-ID in GetPR/ListPRs
- `daemon/handlers/webhook/gerrit.go` - Extract Change-ID from webhook
- `daemon/handlers/prs.go` - Add FindPRByChangeID API

**Implementation:**
1. Add GerritChangeID string field to PRRecord
2. Store Change-ID during PR sync/webhook
3. Add index: gerrit_change_id → pr_id
4. Add API: GET /api/v1/gerrit/change/:change_id → PR record
5. CLI: `asika pr show-by-changeid <change_id>`

---

## Phase 4: Advanced Features (Medium Priority)

### #6: Custom Merge Strategy ⏳
**Files to modify:**
- `common/models/models.go` - Add MergeQueueConfig.MergeStrategyRules
- `daemon/queue/merge.go` - Implement strategy selection logic
- `asika.toml.example` - Document merge_strategy_rules

**Implementation:**
1. Add config: [[merge_queue.merge_strategy_rules]] with pattern/labels/method
2. Match PR labels → select merge method (merge/squash/rebase)
3. Support commit message template: `{{title}} ({{author}})`
4. Fallback to default method if no rule matches

---

### #7: PR Analysis Report ⏳
**Files to create:**
- `daemon/handlers/reports.go` - New file
- `daemon/cron/reports.go` - Scheduled report generator
- Email templates for reports

**Implementation:**
1. Weekly/Monthly stats: PR count, merge time avg, top contributors
2. High-risk PR detection: size > 1000 lines, rebase count > 3
3. Email report via SMTP notifier
4. API: GET /api/v1/reports/weekly?repo_group=xxx
5. CLI: `asika reports generate --period weekly --output report.html`

---

### #8: Conflict Resolution Assistant ⏳
**Files to create:**
- `daemon/handlers/pr/conflicts.go` - Conflict detection API
- Frontend: conflict viewer (simple text editor)

**Implementation:**
1. API: GET /api/v1/repos/:rg/prs/:id/conflicts → {files: [{path, base, head, merged}]}
2. API: POST /api/v1/repos/:rg/prs/:id/resolve-conflict → {file, resolution}
3. Generate conflict report during auto-rebase failures
4. WebUI: 3-column diff viewer (base/head/result)
5. AI suggestion (optional): call LLM API for conflict resolution

---

## Phase 5: Visualization & I18n (Medium Priority)

### #1: PR Dependency Visualization ⏳
**Files to modify:**
- `daemon/handlers/pr_stacks.go` - Add graph export API
- Frontend: Add Mermaid.js or D3.js dependency graph

**Implementation:**
1. API: GET /api/v1/repos/:rg/prs/:id/dependency-graph → Mermaid syntax
2. WebUI: Render Mermaid flowchart on PR detail page
3. Support drag-drop to reorder stack members
4. Show dependency violations (circular deps, blocked by closed PR)

---

### #10: Multi-language WebUI ⏳
**Files to modify:**
- `common/i18n/locales/en.json` - Create/complete English translations
- `common/i18n/locales/ja.json` - Create Japanese translations
- `common/i18n/locales/ko.json` - Create Korean translations
- Frontend templates - Add language switcher dropdown

**Implementation:**
1. Complete en.json (currently only zh.json exists)
2. Add ja.json and ko.json
3. WebUI: Add language dropdown in header
4. Cookie-based language persistence

---

## Phase 6: API & Integration (Low Priority)

### #11: GraphQL API ⏳
**Files to create:**
- `daemon/graphql/schema.go` - GraphQL schema definition
- `daemon/graphql/resolvers.go` - Query/Mutation resolvers
- `daemon/server/routes.go` - Mount /graphql endpoint

**Implementation:**
1. Use github.com/graphql-go/graphql library
2. Schema: Query { pr(id: ID!): PR, prs(repoGroup: String!): [PR] }
3. Schema: Mutation { approvePR(id: ID!): PR, closePR(id: ID!): PR }
4. Endpoint: POST /graphql
5. GraphQL Playground at /graphql/playground

---

### #12: Webhook Event Filtering ⏳
**Files to modify:**
- `common/models/models.go` - Add WebhookFilter config
- `daemon/handlers/webhook/*.go` - Apply event filters
- `asika.toml.example` - Document webhook_filters

**Implementation:**
1. Add config: [[webhook_filters]] with event_types, branch_patterns, ignore_bots
2. Filter events before processing: skip ignored events early
3. Support regex patterns for branch filtering: `^(main|release/.*)$`
4. Option to ignore bot PRs (dependabot, renovate)
5. Metrics: count filtered vs processed events

---

### #13: Auto-labeling System ⏳
**Files to create:**
- `daemon/handlers/auto_label.go` - Auto-labeling logic
- `daemon/cron/auto_label.go` - Periodic label updates

**Implementation:**
1. Size labels: `size/XS` (<10 lines), `size/S` (<100), `size/M` (<500), `size/L` (<1000), `size/XL` (≥1000)
2. File type labels: `lang/go`, `lang/js`, `docs` (*.md), `config` (*.toml/*.yaml)
3. Risk labels: `risk/high` (>1000 lines + <2 reviewers), `needs-review`
4. Stale label: add `stale` if no activity for 30 days
5. Config: [[auto_label_rules]] with pattern/label mappings

---

### #14: CI/CD Integration ⏳
**Files to create:**
- `common/platforms/ci_github.go` - GitHub Actions API client
- `common/platforms/ci_gitlab.go` - GitLab Pipeline API client
- `daemon/handlers/ci_trigger.go` - Manual CI trigger handler

**Implementation:**
1. Trigger GitHub Actions: POST /repos/:owner/:repo/actions/workflows/:id/dispatches
2. Trigger GitLab CI: POST /projects/:id/pipeline
3. Webhook callback handler for Jenkins/CircleCI
4. API: POST /api/v1/repos/:rg/prs/:id/trigger-ci
5. CLI: `asika pr trigger-ci <group> <id>`
6. Auto-retry failed CI (configurable, max 3 retries)

---

## Phase 7: Documentation & Testing

### Documentation Updates ⏳
- README.md - Add new commands and features
- PROJECT.md - Update architecture diagram
- ChangeLog.md - Document all changes
- asika.toml.example - Add new config sections

### Testing ⏳
- Unit tests for all new handlers
- Integration tests for batch operations
- E2E tests for priority queue
- Performance tests for GraphQL queries

---

## Implementation Order

**Week 1:**
- #2 Merge Queue Priority (2 days)
- #3 Batch Operations (3 days)

**Week 2:**
- #4 PR Template Enhancement (2 days)
- #5 Gerrit Change-ID Mapping (2 days)
- #9 Draft PR Workflow (1 day)

**Week 3:**
- #6 Custom Merge Strategy (2 days)
- #8 Conflict Resolution (3 days)

**Week 4:**
- #7 PR Analysis Report (3 days)
- #1 Dependency Visualization (2 days)

**Week 5:**
- #10 Multi-language WebUI (3 days)
- #11 GraphQL API (2 days)

**Week 6:**
- #14 CI/CD Integration (3 days)
- Documentation & Testing (2 days)

---

## Progress Tracking

Total tasks: 12
Completed: 0
In Progress: 0
Pending: 12
