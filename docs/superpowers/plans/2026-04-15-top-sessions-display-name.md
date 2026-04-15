# Top Sessions Display Name — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show meaningful derived session names in the "Top Sessions by Cost"
table instead of raw session IDs, and display the session ID on the meta line
below.

**Architecture:** One SQL change in `GetTopSessionsByCost` to use
`COALESCE(NULLIF(display_name,''), NULLIF(first_message,''), project, id)` for
the display name. One Svelte component change to update the truncation limit and
add the session ID to the meta line.

**Tech Stack:** Go (SQLite), Svelte 5, TypeScript

---

## File Map

| File                                                           | Action | Responsibility                         |
| -------------------------------------------------------------- | ------ | -------------------------------------- |
| `internal/db/usage.go`                                         | Modify | SQL COALESCE change (line 733)         |
| `internal/db/usage_test.go`                                    | Modify | Add test for first_message fallback    |
| `frontend/src/lib/components/usage/TopSessionsTable.svelte`    | Modify | Name truncation + session ID meta line |

---

### Task 1: Backend — Add test for display name fallback chain

**Files:**

- Modify: `internal/db/usage_test.go` (after line 1145, after
  `TestGetTopSessionsByCost`)

- [ ] **Step 1: Write the failing test**

Add a new test that creates sessions exercising each fallback level: one with
`display_name` set, one with only `first_message`, one with neither (should fall
back to project), and one with nothing (should fall back to session ID).

Add this test after the existing `TestGetTopSessionsByCost` function (after line
1145):

```go
func TestGetTopSessionsByCost_DisplayNameFallback(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	requireNoError(t, d.UpsertModelPricing([]ModelPricing{{
		ModelPattern:         "claude-sonnet",
		InputPerMTok:         3.0,
		OutputPerMTok:        15.0,
		CacheCreationPerMTok: 3.75,
		CacheReadPerMTok:     0.30,
	}}), "UpsertModelPricing")

	tokenJSON := `{"input_tokens":100,"output_tokens":50,` +
		`"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`

	// Session with display_name set — should use display_name.
	insertSession(t, d, "s-dn", "proj-a", func(s *Session) {
		s.Agent = "claude"
		s.DisplayName = Ptr("My Custom Name")
		s.FirstMessage = Ptr("some first message")
		s.StartedAt = Ptr("2024-06-15T10:00:00Z")
	})
	insertMessages(t, d, Message{
		SessionID: "s-dn", Ordinal: 0,
		Role: "assistant", Timestamp: "2024-06-15T10:01:00Z",
		Model: "claude-sonnet",
		TokenUsage: json.RawMessage(tokenJSON),
	})

	// Session with no display_name — should fall back to first_message.
	insertSession(t, d, "s-fm", "proj-a", func(s *Session) {
		s.Agent = "claude"
		s.FirstMessage = Ptr("fix the login bug")
		s.StartedAt = Ptr("2024-06-15T11:00:00Z")
	})
	insertMessages(t, d, Message{
		SessionID: "s-fm", Ordinal: 0,
		Role: "assistant", Timestamp: "2024-06-15T11:01:00Z",
		Model: "claude-sonnet",
		TokenUsage: json.RawMessage(tokenJSON),
	})

	// Session with no display_name and no first_message — should
	// fall back to project.
	insertSession(t, d, "s-proj", "my-project", func(s *Session) {
		s.Agent = "claude"
		s.StartedAt = Ptr("2024-06-15T12:00:00Z")
	})
	insertMessages(t, d, Message{
		SessionID: "s-proj", Ordinal: 0,
		Role: "assistant", Timestamp: "2024-06-15T12:01:00Z",
		Model: "claude-sonnet",
		TokenUsage: json.RawMessage(tokenJSON),
	})

	top, err := d.GetTopSessionsByCost(ctx, UsageFilter{
		From: "2024-06-01",
		To:   "2024-06-30",
	}, 20)
	requireNoError(t, err, "GetTopSessionsByCost fallback")

	if len(top) != 3 {
		t.Fatalf("got %d entries, want 3", len(top))
	}

	// Build a map for easy lookup (order is by cost, all equal
	// here so secondary sort is by session ID).
	byID := make(map[string]TopSessionEntry)
	for _, e := range top {
		byID[e.SessionID] = e
	}

	if got := byID["s-dn"].DisplayName; got != "My Custom Name" {
		t.Errorf("s-dn DisplayName = %q, want %q",
			got, "My Custom Name")
	}
	if got := byID["s-fm"].DisplayName; got != "fix the login bug" {
		t.Errorf("s-fm DisplayName = %q, want %q",
			got, "fix the login bug")
	}
	if got := byID["s-proj"].DisplayName; got != "my-project" {
		t.Errorf("s-proj DisplayName = %q, want %q",
			got, "my-project")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/db/ -run TestGetTopSessionsByCost_DisplayNameFallback -v
```

Expected: FAIL — `s-fm` will have `DisplayName = "s-fm"` (the session ID)
instead of `"fix the login bug"`, and `s-proj` will have `DisplayName =
"s-proj"` instead of `"my-project"`.

- [ ] **Step 3: Commit the failing test**

```bash
git add internal/db/usage_test.go
git commit -m "test: add display name fallback test for GetTopSessionsByCost"
```

---

### Task 2: Backend — Change SQL COALESCE in GetTopSessionsByCost

**Files:**

- Modify: `internal/db/usage.go:733`

- [ ] **Step 1: Update the SQL query**

In `internal/db/usage.go`, change line 733 from:

```go
	COALESCE(s.display_name, s.id),
```

to:

```go
	COALESCE(NULLIF(s.display_name, ''), NULLIF(s.first_message, ''), s.project, s.id),
```

- [ ] **Step 2: Run the new test to verify it passes**

Run:

```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/db/ -run TestGetTopSessionsByCost_DisplayNameFallback -v
```

Expected: PASS

- [ ] **Step 3: Run all existing top sessions tests to verify no regressions**

Run:

```bash
CGO_ENABLED=1 go test -tags fts5 ./internal/db/ -run TestGetTopSessionsByCost -v
```

Expected: All `TestGetTopSessionsByCost*` tests PASS. The existing test sets
`DisplayName` explicitly so it still gets priority via the COALESCE.

- [ ] **Step 4: Run go vet and fmt**

```bash
go fmt ./... && go vet ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/db/usage.go
git commit -m "feat: use display_name/first_message fallback in top sessions query"
```

---

### Task 3: Frontend — Update TopSessionsTable display

**Files:**

- Modify:
  `frontend/src/lib/components/usage/TopSessionsTable.svelte:42-50`

- [ ] **Step 1: Update the name truncation and add session ID to meta line**

In `frontend/src/lib/components/usage/TopSessionsTable.svelte`, replace the
session-info div (lines 41-51):

```svelte
          <div class="session-info">
            <span class="session-label">
              <span class="agent-pill">
                {formatAgentName(row.agent)}
              </span>
              {truncate(row.displayName || row.sessionId.slice(0, 12), 40)}
            </span>
            <span class="session-project">
              {row.project}
            </span>
          </div>
```

with:

```svelte
          <div class="session-info">
            <span class="session-label">
              <span class="agent-pill">
                {formatAgentName(row.agent)}
              </span>
              {truncate(row.displayName || row.sessionId.slice(0, 12), 100)}
            </span>
            <span class="session-project">
              {row.project} &middot; {row.sessionId}
            </span>
          </div>
```

Changes:

1. Truncation limit: `40` -> `100`
2. Meta line: `{row.project}` -> `{row.project} · {row.sessionId}`

- [ ] **Step 2: Build the frontend to verify no compile errors**

Run:

```bash
cd frontend && npm run build
```

Expected: Build succeeds with no errors.

- [ ] **Step 3: Start dev servers and verify visually**

Run backend and frontend dev servers:

```bash
make dev        # in one terminal
make frontend-dev  # in another terminal
```

Navigate to the usage/analytics page. Verify:

1. Sessions with a `display_name` show that name on the primary line
2. Sessions without `display_name` show `first_message` content instead of a
   UUID
3. The meta line shows `project · full-session-id` with the session ID
   truncated by CSS ellipsis when it overflows
4. Clicking a row still navigates to the session detail page

- [ ] **Step 4: Commit**

```bash
git add frontend/src/lib/components/usage/TopSessionsTable.svelte
git commit -m "feat: show derived session name and ID in top sessions table"
```
