# Asika Permission Model

## Roles

Asika has three role levels, ordered by increasing privilege:

| Role | Description |
|------|-------------|
| `admin` | Full system access. Bypasses all permission checks including repo group/repo access controls. |
| `operator` | Standard user. Can perform actions granted by granular permissions. Subject to repo group/repo access controls. |
| `viewer` | Read-only. Can view resources but cannot perform mutations. Subject to repo group/repo access controls. |

Role hierarchy: `admin` > `operator` > `viewer`.

A user with role X can perform all actions available to roles below X. For example, an `operator` can perform all `viewer` actions.

## Granular Permissions

`operator`-role users can be granted individual permissions. These are stored in the `permissions` field of the user record and on API keys.

| Permission | Minimum Role | Description |
|------------|-------------|-------------|
| `can_approve` | operator | Approve PRs |
| `can_merge` | operator | Merge PRs |
| `can_close` | operator | Close PRs |
| `can_reopen` | operator | Reopen PRs |
| `can_spam` | operator | Mark PRs as spam |
| `can_manage_queue` | operator | Manage the merge queue |
| `can_revert` | operator | Revert PRs |
| `can_comment` | viewer | Comment on PRs |
| `can_label` | operator | Add/remove labels on PRs |

`admin` users bypass all granular permission checks. `viewer`-role users cannot be granted elevated granular permissions.

## Resource Scopes

### Repo Groups

Each user (except `admin`) can be assigned a list of accessible repo groups via `allowed_repo_groups`. An empty list means access to all repo groups. A non-empty list restricts the user to only those groups.

### Repos

Within a repo group, access can be further restricted to specific repos via `allowed_repos`. An empty list means access to all repos in the allowed groups. A non-empty list restricts to only the specified `owner/repo` entries.

### Spaces (Team Spaces)

A space groups repo groups and has its own member list. Users must be members of a space to access its repo groups (unless they are `admin`). Space roles are separate from system roles.

## API Keys

API keys carry their own role, allowed repo groups, allowed repos, and granular permissions. They authenticate via `X-API-Key` header. The username format for API key auth is `apikey:<key_name>`.

API key permission checks are evaluated before user-based checks. If an API key has a granular permission enabled, the action is allowed regardless of the creator's permissions.

## Temporary Tokens

Temporary tokens (created via `POST /api/v1/auth/temp-token`) carry a subset of the creator's permissions. The creator must have a role level sufficient for each requested permission. Temp tokens expire after a configurable duration (max 24h).

## Middleware Enforcement Order

1. `AuthMiddleware` — authenticates via JWT or API key, sets `username`, `role`, and scope fields on the gin context.
2. `RequireRole` — checks minimum role level.
3. `RequireRepoGroupAccess` — checks repo group access (admins bypass).
4. `RequireRepoAccess` — checks repo-level access (admins bypass).
5. `RequireSpaceAccess` — checks space membership (admins bypass).
6. `RequirePermission` — checks granular permission (admins bypass; API keys checked first, then JWT user DB record, then temp token permissions).

## Permission Check Matrix

| Action | Admin | Operator (with perm) | Operator (no perm) | Viewer |
|--------|-------|---------------------|-------------------|--------|
| View PRs | Yes | Yes | Yes | Yes |
| Approve PR | Yes | Yes (can_approve) | No | No |
| Merge PR | Yes | Yes (can_merge) | No | No |
| Close PR | Yes | Yes (can_close) | No | No |
| Reopen PR | Yes | Yes (can_reopen) | No | No |
| Mark spam | Yes | Yes (can_spam) | No | No |
| Manage queue | Yes | Yes (can_manage_queue) | No | No |
| Revert PR | Yes | Yes (can_revert) | No | No |
| Comment | Yes | Yes (can_comment) | Yes (can_comment) | Yes (can_comment) |
| Label | Yes | Yes (can_label) | No | No |
| Manage users | Yes | No | No | No |
| Manage config | Yes | No | No | No |
| Manage API keys | Yes | No | No | No |
