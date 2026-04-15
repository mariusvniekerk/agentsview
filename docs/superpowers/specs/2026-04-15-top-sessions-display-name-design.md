# Top Sessions by Cost: Session Name Display

## Problem

The "Top Sessions by Cost" table shows raw session IDs (UUIDs/ULIDs) as the
primary identifier for each row. These are meaningless to users. The sessions
sidebar already derives meaningful names from `display_name` or `first_message`,
but the top sessions table falls back to the session ID.

## Solution

Show a meaningful derived name as the primary line and move the session ID to the
meta line below, alongside the project name.

## Changes

### Backend: SQL query in `GetTopSessionsByCost`

**File:** `internal/db/usage.go`, line 733

Change the display name derivation from:

```sql
COALESCE(s.display_name, s.id)
```

to:

```sql
COALESCE(NULLIF(s.display_name, ''), NULLIF(s.first_message, ''), s.project, s.id)
```

`NULLIF` skips empty strings, not just NULLs. The fallback chain is:
`display_name` -> `first_message` -> `project` -> `session_id`.

No struct or API type changes needed. `TopSessionEntry` already has both
`sessionId` and `displayName` as separate fields.

### Frontend: `TopSessionsTable.svelte`

**File:** `frontend/src/lib/components/usage/TopSessionsTable.svelte`

1. **Name line:** Change truncation from 40 to 100 characters. The top sessions
   table has more horizontal space than the sidebar (which uses 50).

2. **Meta line:** Add the session ID alongside the project, separated by a dot:
   ```
   {row.project} · {row.sessionId}
   ```

3. **CSS:** The session ID on the meta line uses the full value. The
   `.session-project` class already has `white-space: nowrap; overflow: hidden;
   text-overflow: ellipsis` — the session ID inherits this behavior. No
   hardcoded character truncation on the ID.

### What does NOT change

- `TopSessionEntry` Go struct — no new fields
- `TopSessionEntry` TypeScript interface — no new fields
- API endpoint `/usage/top-sessions` — same response shape
- Session sidebar logic — no changes to `SessionItem.svelte`

## Design decisions

- **Backend COALESCE over frontend logic:** The sidebar uses JS-side fallback
  with teammate-message XML stripping. The top sessions table uses a simpler
  SQL-only fallback. The teammate parsing is presentation logic specific to the
  sidebar and not worth duplicating.

- **CSS truncation for session ID:** The full session ID is sent to the frontend.
  CSS `text-overflow: ellipsis` on the meta line handles truncation responsively
  based on available width, rather than hardcoding a character limit.

- **100-char JS truncation on name:** The top sessions table is wider than the
  sidebar, so we use 100 instead of the sidebar's 50. CSS ellipsis is a second
  safety net.
