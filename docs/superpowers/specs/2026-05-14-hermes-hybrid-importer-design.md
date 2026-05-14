# Hermes Hybrid Importer Design

## Context

Agentsview currently treats Hermes as a file-backed transcript agent. It discovers
files under `~/.hermes/sessions`, parses `session_*.json` and `*.jsonl`, then writes
ordinary `sessions`, `messages`, and `tool_calls` rows. That imports visible
conversation content, but it misses metadata that Hermes stores in its canonical
SQLite state database.

The local archive shows the gap clearly:

- Agentsview has 103 Hermes sessions and 5,209 Hermes messages.
- No Hermes message has `model` or `token_usage`.
- No Hermes session has `parent_session_id` or `relationship_type`.
- Many Hermes transcripts contain compaction handoff text, but those rows are
  imported as normal user messages.
- Hermes `~/.hermes/state.db` contains model, title, parent links, token totals,
  cache tokens, reasoning tokens, API call counts, billing status, and many
  full message rows.

The best importer should use both sources: `state.db` for the spine of the
session graph and accounting data, plus transcript files for complete message
content when they are richer than the database copy.

## Goals

- Preserve the fullest available Hermes transcript content.
- Import Hermes continuation chains using real `parent_session_id` metadata.
- Import Hermes model and token usage without inventing fake per-message
  accounting.
- Keep transcript-only Hermes archives working.
- Avoid destructive archive migrations. Existing session data must be preserved
  during resync.
- Fit the existing Agentsview parser/sync/store architecture.

## Non-Goals

- Do not modify Hermes itself as part of this design.
- Do not delete or recreate the local Agentsview SQLite database.
- Do not infer detailed per-turn token usage when Hermes only provides
  session-level totals.
- Do not redesign the whole usage UI. The importer should supply better data;
  UI refinements can remain small and follow existing patterns.

## Recommended Approach

Implement a hybrid Hermes importer. It should build a per-session bundle from all
available Hermes sources, choose the best source for each type of data, and emit
ordinary `parser.ParseResult` values plus session-level usage data.

The source precedence is:

1. Use `~/.hermes/state.db.sessions` for identity, title, source/platform,
   model, parent relationship, timestamps, token totals, billing status, and
   API call count.
2. Use `~/.hermes/state.db.messages` as the preferred structured message stream
   when it is complete enough.
3. Use `~/.hermes/sessions/session_<id>.json` when it has more messages or more
   message content than `state.db.messages`.
4. Use `~/.hermes/sessions/<id>.jsonl` when neither state messages nor JSON
   transcript is available.

The importer must not blindly append messages from multiple sources. It should
choose one primary message stream per session, then enrich it from the other
sources only when rows can be aligned confidently.

## Architecture

Add a Hermes archive import path alongside the current transcript-file parser.
The existing `ParseHermesSession(path, project, machine)` remains as the fallback
for copied transcript directories or older Hermes archives without `state.db`.

New parser responsibilities:

- Discover Hermes roots that contain `state.db` and/or `sessions/`.
- Read `state.db` read-only.
- Build session bundles keyed by raw Hermes session ID.
- Select metadata and message sources for each bundle.
- Emit `ParsedSession` and `ParsedMessage` values using the same shape as other
  agents.
- Emit session-level usage records from the parser layer and persist them through
  the sync/store layer alongside the session write.

Suggested internal types:

```go
type HermesSessionBundle struct {
    ID              string
    StateSession    *HermesStateSession
    StateMessages   []HermesStateMessage
    JSONTranscript  *HermesJSONTranscript
    JSONLTranscript []HermesJSONLMessage
}

type HermesMessageSource string

const (
    HermesMessagesState HermesMessageSource = "state"
    HermesMessagesJSON  HermesMessageSource = "json"
    HermesMessagesJSONL HermesMessageSource = "jsonl"
)
```

The exact type names can vary, but the boundary should remain clear: discovery
finds sources, bundling reconciles them, parsing converts a selected bundle into
Agentsview domain objects.

## Session Metadata Mapping

When a state row exists, map it as follows:

- `sessions.id`: `hermes:<state.id>`
- `sessions.agent`: `hermes`
- `sessions.project`: `hermes-<source>` when `source` is available, otherwise
  `hermes`
- `sessions.display_name`: `state.title`
- `sessions.started_at`: `state.started_at`
- `sessions.ended_at`: `state.ended_at`
- `sessions.parent_session_id`: `hermes:<state.parent_session_id>` when present
- `sessions.relationship_type`: `continuation` when parent is present unless a
  future Hermes field identifies a more specific relationship
- `sessions.source_session_id`: raw Hermes ID
- `sessions.source_version`: `hermes-state-db-v<schema_version>` for state-db
  imports; `hermes-transcript` for transcript-only fallback imports
- `sessions.file_path`: retain the selected transcript file path when present;
  otherwise point to `state.db` for DB-only sessions

This immediately fixes the UI split where one logical Hermes conversation appears
as many independent sessions after compaction.

## Message Source Selection

For each bundle, compute a small quality score for every available message source:

- Message count.
- Total visible content length.
- Presence of tool calls.
- Presence of reasoning fields.
- Timestamp coverage.

Prefer state messages when counts and content are comparable because they carry
structured reasoning and tool metadata. Prefer JSON transcript when it has more
messages or materially more content. Prefer JSONL only when it is the only
available complete source.

The local archive demonstrates why this matters: one recent Hermes session has
187 state messages but 212 JSON transcript messages. The JSON transcript should
win for visible content in that case.

## Message Conversion

All selected message streams should convert into the existing `ParsedMessage`
shape:

- User content becomes `RoleUser`.
- Assistant content becomes `RoleAssistant`.
- Hermes `reasoning`, `reasoning_content`, `reasoning_details`, and
  `codex_reasoning_items` become thinking text or the existing Hermes thinking
  display format.
- Tool calls become `ParsedToolCall` values with normalized categories.
- Tool result rows become `ParsedToolResult` values so existing
  `pairToolResults` can attach output to tool calls.

Compaction handoff messages should be classified instead of treated as normal
user prompts. Messages starting with known Hermes compaction handoff prefixes,
including `[CONTEXT COMPACTION - REFERENCE ONLY]` and the Unicode dash variant,
should be imported as system boundary messages:

- `IsSystem = true`
- `SourceType = "system"`
- `SourceSubtype = "compact_boundary"`
- `IsCompactBoundary = true`

These rows should not count toward `user_message_count` or first-message
selection.

## Usage Data

Hermes usage is currently session-level. Agentsview usage aggregation is
message-level today and filters for non-empty `messages.model` and
`messages.token_usage`. For Hermes, forcing aggregate totals onto one synthetic
message would make the numbers show up quickly, but it would misrepresent the
source of truth.

The cleaner design is to add session-level usage events and teach usage queries
to read both sources. Message-level usage remains stored only on `messages`.
Hermes session-level usage is stored only in `usage_events`. Reporting queries
normalize both into a common row shape with `UNION ALL`; they do not duplicate
existing message-level usage into the new table.

Suggested table:

```sql
CREATE TABLE IF NOT EXISTS usage_events (
    id INTEGER PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    message_ordinal INTEGER,
    source TEXT NOT NULL,
    model TEXT NOT NULL,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd REAL,
    cost_status TEXT NOT NULL DEFAULT '',
    cost_source TEXT NOT NULL DEFAULT '',
    occurred_at TEXT,
    dedup_key TEXT NOT NULL DEFAULT ''
);
```

Hermes writes one `source = "session"` event per state session using:

- `input_tokens`
- `output_tokens`
- `cache_read_tokens`
- `cache_write_tokens` mapped to `cache_creation_input_tokens`
- `reasoning_tokens`
- `estimated_cost_usd` or `actual_cost_usd`
- `cost_status`
- `cost_source`
- `model`

Usage queries should build a normalized usage-row stream with two arms:

```sql
SELECT
    m.session_id,
    m.ordinal AS message_ordinal,
    'message' AS usage_source,
    COALESCE(m.timestamp, s.started_at) AS occurred_at,
    m.model,
    m.token_usage,
    0 AS input_tokens,
    0 AS output_tokens,
    0 AS cache_creation_input_tokens,
    0 AS cache_read_input_tokens,
    0 AS reasoning_tokens,
    NULL AS cost_usd,
    '' AS cost_status,
    '' AS cost_source,
    m.claude_message_id,
    m.claude_request_id,
    '' AS usage_dedup_key,
    s.project,
    s.agent,
    s.machine,
    s.user_message_count,
    s.is_automated
FROM messages m
JOIN sessions s ON s.id = m.session_id
WHERE m.token_usage != ''
  AND m.model != ''
  AND m.model != '<synthetic>'
  AND s.deleted_at IS NULL

UNION ALL

SELECT
    ue.session_id,
    ue.message_ordinal,
    ue.source AS usage_source,
    COALESCE(ue.occurred_at, s.started_at) AS occurred_at,
    ue.model,
    '' AS token_usage,
    ue.input_tokens,
    ue.output_tokens,
    ue.cache_creation_input_tokens,
    ue.cache_read_input_tokens,
    ue.reasoning_tokens,
    ue.cost_usd,
    ue.cost_status,
    ue.cost_source,
    '' AS claude_message_id,
    '' AS claude_request_id,
    COALESCE(NULLIF(ue.dedup_key, ''), ue.source || ':' || ue.session_id)
        AS usage_dedup_key,
    s.project,
    s.agent,
    s.machine,
    s.user_message_count,
    s.is_automated
FROM usage_events ue
JOIN sessions s ON s.id = ue.session_id
WHERE ue.model != ''
  AND s.deleted_at IS NULL
```

`internal/db/usage.go` and `internal/postgres/usage.go` should then apply the
same date, model, agent, project, machine, user-message, one-shot, automated,
and active-since filters to the unified stream's normalized aliases rather than
directly to `messages` columns. The existing Go-side `token_usage` parsing
remains the path for `usage_source = "message"` rows. `usage_source !=
"message"` rows use the scalar token columns directly and use `cost_usd` when
Hermes supplies a trusted cost, falling back to model pricing when it does not.

Existing Claude/Codex usage keeps its message-level dedup behavior based on
`claude_message_id` plus `claude_request_id`. Hermes events use
`usage_events.dedup_key` when present and otherwise naturally count once per
session event. This preserves the current message-level behavior for other
agents while making Hermes visible on usage dashboards without fabricating
per-message accounting.

## Sync And Resync Behavior

Hermes state-db imports should follow the repository's data-safety rule:
schema changes use `ALTER TABLE`; parser changes trigger a full resync through
the existing fresh-DB-and-atomic-swap path. Existing sessions that no longer
exist on disk must be preserved through the orphan-copy process.

The importer should use read-only SQLite connections for Hermes `state.db`.
If `state.db` is locked, missing, or has an unsupported schema, sync should fall
back to transcript files and record a warning rather than failing the whole sync.

## How This Improves Agentsview

Session list:

- Before: compaction continuations appear as separate root sessions.
- After: continuations group under the original Hermes session using existing
  frontend grouping logic.

Usage:

- Before: Hermes contributes no usage or pricing because every Hermes message has
  empty `model` and `token_usage`.
- After: Hermes contributes token totals, cache-read volume, reasoning tokens,
  API-call counts, and billing status through session-level usage events that
  are unioned with existing message-level usage at query time.

Transcript quality:

- Before: Agentsview imports whichever transcript file the current discovery path
  sees and misses richer DB metadata.
- After: metadata and usage come from state DB, while visible transcript content
  can still come from the fuller JSON transcript.

Compaction display:

- Before: compaction handoff text is treated as a real user message.
- After: compaction handoff rows become system boundaries and no longer distort
  user-message counts, first-message summaries, or health metrics.

Compatibility:

- Before: Hermes transcript-only archives work.
- After: transcript-only archives still work, while local Hermes installs with
  `state.db` get richer imports.

## Error Handling

- Missing `state.db`: parse transcript files exactly as today.
- Missing transcript file for a state session: import DB metadata and DB messages
  if present.
- State DB has fewer messages than transcript: prefer transcript messages while
  still applying state metadata and usage.
- Parent session missing from the imported set: store the parent ID anyway; the
  existing UI can tolerate unloaded parents, and a later sync may import it.
- Unsupported Hermes schema: log a warning and fall back to transcript parsing.
- Invalid usage numbers: clamp negative token fields to zero and leave
  `cost_usd` null when no reliable cost exists.

## Testing Plan

Backend tests should include:

- A state-db fixture with parent links imports `relationship_type =
  "continuation"` and prefixed parent IDs.
- A bundle where JSON transcript has more messages than state DB chooses JSON
  messages.
- A bundle where state DB is the only source imports DB messages.
- Transcript-only JSON and JSONL fixtures still import through the fallback path.
- Compaction handoff text becomes a system compact boundary and does not increase
  `user_message_count`.
- Hermes session-level usage appears in daily usage totals and top-session usage
  without populating fake per-message `token_usage`.
- Existing Claude, Codex, and other message-level usage still comes from
  `messages.token_usage`, is not copied into `usage_events`, and does not
  double-count when the unified usage stream is queried.
- A mixed fixture with one message-level agent session and one Hermes
  `usage_events` row returns the sum of both sources in daily totals, top
  sessions, model breakdowns, project breakdowns, and agent breakdowns.
- State DB lock or schema mismatch falls back to transcript files without failing
  the entire sync.

Frontend tests can stay focused on existing grouping utilities:

- Hermes continuation sessions group like other continuation chains when
  `parent_session_id` and `relationship_type` are present.
- Usage filters include Hermes session-level usage in agent and project
  breakdowns.

## Rollout

1. Add the hybrid parser path and tests with transcript fallback preserved.
2. Add session-level usage storage plus `UNION ALL` query support.
3. Wire Hermes state-db usage into that usage path.
4. Trigger a full resync so existing Hermes rows are rebuilt with parent links,
   compact-boundary metadata, and usage events.

The first implementation should avoid UI redesign. Existing sidebar grouping and
usage pages should benefit from better backend data with minimal frontend change.
