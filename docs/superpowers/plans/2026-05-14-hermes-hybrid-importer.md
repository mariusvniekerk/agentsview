# Hermes Hybrid Importer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Import Hermes state-db metadata, continuations, compact boundaries, and session-level usage without duplicating existing message-level usage.

**Architecture:** Add `usage_events` as a separate storage source, then make usage queries consume a normalized union of message rows and usage-event rows. Extend Hermes parsing to bundle `state.db` rows with transcript files, choosing the best message source while preserving state metadata and usage events.

**Tech Stack:** Go, SQLite, PostgreSQL, `gjson`, standard `database/sql`, existing parser/sync/db patterns.

---

### Task 1: Usage Events Storage

**Files:**
- Modify: `internal/db/schema.sql`
- Modify: `internal/db/db.go`
- Create: `internal/db/usage_events.go`
- Modify: `internal/db/usage_test.go`
- Modify: `internal/parser/types.go`

- [ ] **Step 1: Write failing SQLite storage tests**

Add tests in `internal/db/usage_test.go`:

```go
func TestUsageEventsReplaceAndList(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	insertUsageSession(t, d, "hermes:event", "hermes", "proj", "2026-05-14T10:00:00Z", 2)

	events := []UsageEvent{{
		SessionID: "hermes:event",
		Source: "session",
		Model: "gpt-5.4",
		InputTokens: 100,
		OutputTokens: 50,
		CacheCreationInputTokens: 7,
		CacheReadInputTokens: 11,
		ReasoningTokens: 13,
		CostUSD: floatPtr(0.02),
		CostStatus: "estimated",
		CostSource: "hermes",
		OccurredAt: "2026-05-14T10:05:00Z",
		DedupKey: "session:hermes:event",
	}}
	if err := d.ReplaceSessionUsageEvents("hermes:event", events); err != nil {
		t.Fatalf("ReplaceSessionUsageEvents: %v", err)
	}
	got, err := d.GetUsageEvents(ctx, "hermes:event")
	if err != nil {
		t.Fatalf("GetUsageEvents: %v", err)
	}
	if len(got) != 1 || got[0].InputTokens != 100 || got[0].CostUSD == nil {
		t.Fatalf("usage events not round-tripped: %#v", got)
	}
	if err := d.ReplaceSessionUsageEvents("hermes:event", nil); err != nil {
		t.Fatalf("ReplaceSessionUsageEvents clear: %v", err)
	}
	got, err = d.GetUsageEvents(ctx, "hermes:event")
	if err != nil {
		t.Fatalf("GetUsageEvents after clear: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("usage events after clear = %d, want 0", len(got))
	}
}
```

Expected red result: compile failure because `UsageEvent`, `ReplaceSessionUsageEvents`, and `GetUsageEvents` do not exist.

- [ ] **Step 2: Add schema and migration**

Add `usage_events` to `internal/db/schema.sql` and `migrateColumns`:

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
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_events_dedup
    ON usage_events(session_id, source, dedup_key)
    WHERE dedup_key != '';
CREATE INDEX IF NOT EXISTS idx_usage_events_session
    ON usage_events(session_id);
CREATE INDEX IF NOT EXISTS idx_usage_events_occurred
    ON usage_events(occurred_at);
```

- [ ] **Step 3: Add DB API**

Create `internal/db/usage_events.go` with:

```go
type UsageEvent struct {
	ID int64
	SessionID string
	MessageOrdinal *int
	Source string
	Model string
	InputTokens int
	OutputTokens int
	CacheCreationInputTokens int
	CacheReadInputTokens int
	ReasoningTokens int
	CostUSD *float64
	CostStatus string
	CostSource string
	OccurredAt string
	DedupKey string
}

func (db *DB) ReplaceSessionUsageEvents(sessionID string, events []UsageEvent) error
func (db *DB) GetUsageEvents(ctx context.Context, sessionID string) ([]UsageEvent, error)
```

The replace method must delete existing rows for the session and insert the provided events in one transaction.

- [ ] **Step 4: Run targeted storage test**

Run:

```bash
go test ./internal/db -run TestUsageEventsReplaceAndList -count=1
```

Expected green result: the new storage test passes.

- [ ] **Step 5: Commit**

```bash
git add internal/db/schema.sql internal/db/db.go internal/db/usage_events.go internal/db/usage_test.go internal/parser/types.go
git commit -m "feat: add usage event storage"
```

### Task 2: Union Usage Queries

**Files:**
- Modify: `internal/db/usage.go`
- Modify: `internal/db/usage_test.go`
- Modify: `internal/postgres/usage.go`

- [ ] **Step 1: Write failing mixed-source usage tests**

Add tests proving message usage and Hermes event usage are summed once:

```go
func TestGetDailyUsageUnionsMessageAndUsageEvents(t *testing.T) {
	ctx := context.Background()
	d := openTestDB(t)
	insertUsageSession(t, d, "claude:msg", "claude", "proj-a", "2026-05-14T09:00:00Z", 2)
	insertUsageMessage(t, d, "claude:msg", "claude-sonnet-4-20250514", `{"input_tokens":100,"output_tokens":40}`)
	insertUsageSession(t, d, "hermes:event", "hermes", "proj-b", "2026-05-14T10:00:00Z", 2)
	requireNoError(t, d.ReplaceSessionUsageEvents("hermes:event", []UsageEvent{{
		SessionID: "hermes:event", Source: "session", Model: "gpt-5.4",
		InputTokens: 300, OutputTokens: 70, CacheReadInputTokens: 20,
		OccurredAt: "2026-05-14T10:05:00Z", DedupKey: "session:hermes:event",
	}}), "replace hermes usage event")

	got, err := d.GetDailyUsage(ctx, UsageFilter{From: "2026-05-14", To: "2026-05-14", Breakdowns: true})
	requireNoError(t, err, "GetDailyUsage")
	if got.Totals.InputTokens != 400 || got.Totals.OutputTokens != 110 || got.Totals.CacheReadTokens != 20 {
		t.Fatalf("totals = %#v", got.Totals)
	}
}
```

Expected red result: Hermes event tokens are missing.

- [ ] **Step 2: Refactor query rows**

Introduce a private `usageRow` struct and helper query builder in `internal/db/usage.go` that returns the union stream. For `usage_source = "message"`, parse `token_usage`; for other rows, use scalar columns.

- [ ] **Step 3: Apply union stream to all three queries**

Update `GetDailyUsage`, `GetTopSessionsByCost`, and `GetUsageSessionCounts` to consume the shared stream. Preserve Claude dedup with `claude_message_id` plus `claude_request_id`; apply `usage_dedup_key` for event rows.

- [ ] **Step 4: Run usage tests**

Run:

```bash
go test ./internal/db -run 'TestGetDailyUsageUnionsMessageAndUsageEvents|TestGetTopSessions|TestGetUsageSessionCounts|TestUsageQueryEligibilityParity' -count=1
```

Expected green result: existing message-level tests and mixed-source tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/db/usage.go internal/db/usage_test.go internal/postgres/usage.go
git commit -m "feat: union usage events into usage queries"
```

### Task 3: Hermes State DB Parser

**Files:**
- Modify: `internal/parser/types.go`
- Modify: `internal/parser/hermes.go`
- Modify: `internal/parser/hermes_test.go`

- [ ] **Step 1: Write failing Hermes state-db tests**

Add fixtures in `internal/parser/hermes_test.go` that create a temp Hermes root with `state.db` and `sessions/session_child.json`.

Required assertions:
- `DiscoverHermesSessions(root)` returns one state-backed source when `root/state.db` exists.
- parsed child session has `ID = "hermes:child"`, `ParentSessionID = "hermes:parent"`, `RelationshipType = RelContinuation`, `DisplayName` from `sessions.title`, `Project = "hermes-discord"`, and one `UsageEvent`.
- when JSON transcript has more messages than `state.db.messages`, JSON is chosen for visible messages.
- compaction text becomes `IsSystem`, `SourceType = "system"`, `SourceSubtype = "compact_boundary"`, `IsCompactBoundary = true`, and is excluded from user message count.

Expected red result: state-db discovery and usage events do not exist.

- [ ] **Step 2: Add parser usage type**

Extend `parser.ParseResult`:

```go
type ParseResult struct {
	Session ParsedSession
	Messages []ParsedMessage
	UsageEvents []ParsedUsageEvent
}
```

Add `ParsedUsageEvent` mirroring `db.UsageEvent` without DB ids.

- [ ] **Step 3: Add Hermes bundling**

Implement state-backed discovery and parsing:

```go
func ParseHermesRoot(root, project, machine string) ([]ParseResult, error)
func parseHermesStateDB(path, sessionsDir, project, machine string) ([]ParseResult, error)
```

Use read-only SQLite, map state sessions to Agentsview sessions, choose JSON transcript over state messages when it has materially more messages/content, and fall back to existing JSON/JSONL parsers when `state.db` is absent.

- [ ] **Step 4: Run Hermes parser tests**

Run:

```bash
go test ./internal/parser -run Hermes -count=1
```

Expected green result: all Hermes parser tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/parser/types.go internal/parser/hermes.go internal/parser/hermes_test.go
git commit -m "feat: import hermes state database"
```

### Task 4: Sync And PostgreSQL Parity

**Files:**
- Modify: `internal/sync/engine.go`
- Modify: `internal/sync/engine_integration_test.go`
- Modify: `internal/postgres/schema.go`
- Modify: `internal/postgres/push.go`
- Modify: `internal/postgres/usage.go`
- Modify: `internal/postgres/usage_pgtest_test.go`

- [ ] **Step 1: Write failing sync test**

Add an integration test where the configured Hermes directory is the Hermes root, not only `sessions/`, and assert a sync writes parent linkage, compact boundary flags, and usage events.

- [ ] **Step 2: Persist usage events during sync**

Extend `pendingWrite` with usage events, convert parser usage events to db usage events, and call `ReplaceSessionUsageEvents` after session/message writes succeed. Bulk write should include the same callback.

- [ ] **Step 3: Add PostgreSQL schema and push support**

Add `usage_events` to PG `coreDDL`, idempotent schema migration, push delete/insert for each session, and PG usage query union parity.

- [ ] **Step 4: Run sync and usage tests**

Run:

```bash
go test ./internal/sync -run Hermes -count=1
go test ./internal/db -run Usage -count=1
go test ./internal/postgres -run Usage -count=1
```

Expected result: SQLite and sync tests pass; PG tests pass when PostgreSQL test env is configured, otherwise report the skip/error honestly.

- [ ] **Step 5: Commit**

```bash
git add internal/sync/engine.go internal/sync/engine_integration_test.go internal/postgres/schema.go internal/postgres/push.go internal/postgres/usage.go internal/postgres/usage_pgtest_test.go
git commit -m "feat: sync hermes usage events"
```

### Task 5: Final Verification

**Files:**
- No new source files unless fixing verification failures.

- [ ] **Step 1: Run focused backend tests**

```bash
go test ./internal/parser -run Hermes -count=1
go test ./internal/db -run Usage -count=1
go test ./internal/sync -run Hermes -count=1
```

- [ ] **Step 2: Run broader regression tests**

```bash
go test ./internal/parser ./internal/db ./internal/sync ./internal/server -count=1
```

- [ ] **Step 3: Run static checks**

```bash
gofmt -w internal/parser/hermes.go internal/parser/hermes_test.go internal/parser/types.go internal/db/usage.go internal/db/usage_events.go internal/db/usage_test.go internal/sync/engine.go internal/sync/engine_integration_test.go internal/postgres/schema.go internal/postgres/push.go internal/postgres/usage.go internal/postgres/usage_pgtest_test.go
git diff --check
```

- [ ] **Step 4: Commit fixes if needed**

Commit any verification fixes with a focused message.
