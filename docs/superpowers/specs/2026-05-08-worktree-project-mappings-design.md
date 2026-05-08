# Worktree Project Mappings Design

## Context

Issue 416 reports worktree sessions being grouped under a branch-path leaf instead
of the owning project. The concrete case is a sibling worktree container such as
`.../{project}.worktrees/feat/my-new-feature/`, where the desired project is
`{project}` but the current fallback can resolve to `my-new-feature`.

AgentsView already has a small set of hardcoded worktree layout recognizers for
known tools and local conventions. The sibling `{project}.worktrees` shape is
not clearly established as a default convention across a major tool, so the fix
should not add a broad parser heuristic for it. Users need an explicit way to
configure these mappings from Settings.

## Goals

- Let users map local worktree path prefixes to canonical project names.
- Store mappings in SQLite because sessions are synced and the archive is the
  operational source of truth.
- Scope mappings to the server's current machine without exposing machine
  selection in the UI.
- Keep settings manual-only. Do not infer or suggest mappings.
- Avoid surprising archive rewrites when a user saves a mapping.
- Keep existing filtering, analytics, sync, and project queries based on
  `sessions.project`.

## Non-Goals

- Do not add a built-in `{project}.worktrees` heuristic.
- Do not support cross-machine editing in the Settings UI.
- Do not preserve an original parser-derived project column for undo.
- Do not implement automatic detected suggestions in the first version.
- Do not move this setting into `config.toml`.

## Data Model

Add a SQLite table:

```sql
CREATE TABLE IF NOT EXISTS worktree_project_mappings (
    id INTEGER PRIMARY KEY,
    machine TEXT NOT NULL,
    path_prefix TEXT NOT NULL,
    project TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(machine, path_prefix)
);
```

Indexes:

- `(machine, enabled, path_prefix)` for matching session paths.
- `(machine, project)` for settings display and future diagnostics.

Normalize `path_prefix` on write:

- Trim whitespace.
- Clean path syntax with platform-aware path cleaning.
- Store without a trailing separator except for filesystem roots.
- Reject empty values.

Normalize `project` on write using the same project-name normalization used by
the parser so mapped rows behave like existing project names.

## Matching Semantics

A mapping applies when:

- `mapping.enabled = 1`
- `mapping.machine = session.machine`
- `session.cwd` equals `mapping.path_prefix` or is under it at a path-component
  boundary.

If more than one mapping matches, the longest `path_prefix` wins. This allows a
user to add a broader project mapping and then override a nested subtree later.

Path matching should be string-based after normalization. It should not require
the path to exist on disk because archived sessions often point at deleted
worktrees.

## Sync Behavior

During sync, after normal parser project extraction, resolve the session project
through active mappings for the session's machine and cwd. If a mapping matches,
store the mapped project in `sessions.project`.

This keeps future sync and Full Resync consistent with the settings table while
leaving the parser's built-in layout recognizers focused on known conventions.

## Apply Behavior

Saving a mapping only writes the rule. It does not rewrite existing sessions.

Add an explicit "Apply mappings" operation. It updates existing sessions for the
current machine whose `cwd` matches enabled rules. The operation uses
longest-prefix matching and writes the selected mapped project to
`sessions.project`.

The operation is forward-only. If a mapping is removed or changed, previously
updated rows are not automatically restored. Full Resync is the supported clean
recompute path because it reparses source sessions and applies the current active
mapping set.

## API

Add endpoints under `/api/v1/settings/worktree-mappings`:

- `GET /api/v1/settings/worktree-mappings`
  - Returns mappings for the server's current machine only.
  - Includes the current machine as read-only metadata.
- `POST /api/v1/settings/worktree-mappings`
  - Creates a mapping for the current machine.
  - Request: `path_prefix`, `project`, `enabled`.
  - Machine is ignored if supplied by the client.
- `PUT /api/v1/settings/worktree-mappings/{id}`
  - Updates an existing mapping for the current machine.
  - Rejects mappings owned by any other machine.
- `DELETE /api/v1/settings/worktree-mappings/{id}`
  - Deletes a mapping for the current machine.
- `POST /api/v1/settings/worktree-mappings/apply`
  - Applies enabled mappings to existing sessions for the current machine.
  - Returns counts for matched and updated sessions.

All write endpoints should reject read-only database mode.

## Settings UI

Add a "Worktree Mappings" settings section.

Display:

- Current machine as read-only text.
- Existing mappings for this machine.
- Editable path prefix, project, and enabled fields.
- Add row action.
- Save and delete controls per row.
- Apply mappings button with result counts.

The UI should not expose a machine selector. Each machine configures its own
local path mappings.

Use concise helper text:

- Saving stores the rule only.
- Apply mappings updates existing archived sessions.
- Full Resync recomputes projects from source sessions and current mappings.

## Error Handling

- Reject empty `path_prefix` or `project`.
- Reject duplicate `path_prefix` for the current machine.
- Reject malformed IDs and mappings from other machines with 404.
- Return validation errors as 400 responses.
- Log unexpected persistence or apply failures server-side and return a generic
  500 response.

## Testing

Backend tests:

- Schema migration creates the mapping table without touching existing session
  data.
- CRUD endpoints only operate on current-machine mappings.
- Client-supplied machine is ignored on create; the server writes the current
  machine.
- Duplicate path prefixes are rejected for the same machine.
- Longest-prefix matching wins.
- Matching respects path-component boundaries.
- Apply updates only sessions on the current machine.
- Apply does not update disabled mappings.
- Sync applies mappings during session upsert.

Frontend tests:

- Settings renders current machine as read-only.
- Add, edit, delete, enable, and disable controls call the expected API methods.
- Apply button displays matched/updated counts.
- No machine selector is present.

## Open Questions

None. The agreed design is manual-only SQLite mappings, implicit current-machine
scope, no built-in `{project}.worktrees` heuristic, and explicit apply/resync for
historical rows.
