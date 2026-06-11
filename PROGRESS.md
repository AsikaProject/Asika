# Implementation Progress

## Phase 1: Queue & Batch Operations

### #2: Merge Queue Priority Scheduling ✅
- [x] Created queue_priority.go handler
- [x] Wire route in server/routes.go
- [x] Add priority sorting to queue/manager.go
- [x] Add CLI command (asika queue priority)
- [ ] Add bot commands

### #3: Batch Operations Enhancement ✅
- [x] Created batch_merge.go
- [x] Created batch_cherrypick.go (stub - needs platform API support)
- [x] Created batch_handlers.go
- [x] Wire routes in server/routes.go
- [x] Add CLI commands (asika pr batch-merge, batch-cherrypick)
- [ ] Add bot commands
- [ ] Add WebUI multi-select support

## Phase 2: PR Templates & Workflow

### #4: PR Template and Checklist Enhancement ✅
- [x] Add GetChecklistProgress API
- [x] Wire route (GET /api/v1/repos/:repo_group/prs/:pr_id/checklist)
- [x] Enhanced checklist validation with item details
- [ ] Add bot commands
- [ ] Add WebUI progress bar

### #9: Draft PR Workflow Enhancement ✅
- [x] Add MarkDraft handler
- [x] Wire route (POST /api/v1/repos/:repo_group/prs/:pr_id/draft)
- [x] MarkReady already existed with auto-enqueue support
- [ ] Add CLI commands (asika pr draft, ready)
- [ ] Add bot commands
- [ ] Skip CI trigger for drafts (needs consumer.go changes)

## Phase 3: Platform Enhancements

### #5: Gerrit Change-ID Mapping
- [ ] Add GerritChangeID to models
- [ ] Update gerrit.go platform
- [ ] Add index and lookup API

## Phase 4: Advanced Features

### #6: Custom Merge Strategy ✅
- [x] Add MergeStrategyRule to models.go
- [x] Implement selectMergeMethod in queue/manager.go
- [x] Support label-based rule matching
- [x] Support pattern-based rule matching
- [x] Support priority-based rule ordering
- [x] Add DefaultMergeMethod config option
- [ ] Document in asika.toml.example

## Status: 5/12 tasks completed, 0/12 fully tested

## Notes:
- Priority scheduling: Queue items now sorted by priority (high to low) then by time
- Batch merge: Functional, merges multiple PRs sequentially
- Batch cherry-pick: Stub implementation - requires platform API CherryPickPR method
- Checklist progress: Returns detailed item list with checked/unchecked status
- Draft workflow: Can mark PRs as draft or ready, with auto-enqueue option
- Custom merge strategy: Rules match by labels or patterns, with priority ordering
- CLI commands: queue priority, pr batch-merge, batch-cherrypick available
- Next steps: Add bot commands, implement Gerrit Change-ID mapping, PR analysis reports

