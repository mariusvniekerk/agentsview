# Worktree Project Mappings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add manual, SQLite-backed worktree path-prefix mappings that assign sessions on the current machine to canonical projects during sync or explicit apply.

**Architecture:** Store mappings in SQLite and expose local-only CRUD/apply endpoints under settings. Sync applies active mappings after parser project extraction and before writing `sessions.project`; existing sessions are rewritten only through an explicit apply action. The Svelte settings panel edits mappings for the current machine without exposing a machine selector.

**Tech Stack:** Go, SQLite, `net/http` ServeMux, Svelte 5, TypeScript, Vitest, Playwright-compatible frontend conventions.

---

## File Structure

- Create `internal/db/worktree_mappings.go`: mapping data model, normalization, matching, CRUD, sync resolution, and apply logic.
- Modify `internal/db/schema.sql`: add `worktree_project_mappings` table and indexes to the initial schema.
- Modify `internal/db/db.go`: run idempotent table/index migration for existing databases.
- Create `internal/db/worktree_mappings_test.go`: DB unit tests for schema, validation, matching, machine scoping, longest-prefix resolution, and apply.
- Modify `internal/sync/engine.go`: expose current machine through `Machine()` and call DB mapping resolution in `prepareSessionWrite`.
- Add tests to `internal/sync/engine_integration_test.go`: prove sync stores the mapped project.
- Create `internal/server/worktree_mappings.go`: HTTP handlers and request/response types.
- Modify `internal/server/server.go`: register mapping routes.
- Add tests to `internal/server/server_test.go`: CRUD/apply/read-only/current-machine route coverage.
- Modify `frontend/src/lib/api/client.ts`: mapping API types and functions.
- Create `frontend/src/lib/components/settings/WorktreeMappingsSettings.svelte`: settings panel.
- Modify `frontend/src/lib/components/settings/SettingsPage.svelte`: render the new panel.
- Create `frontend/src/lib/components/settings/WorktreeMappingsSettings.test.ts`: component behavior tests.

## Task 1: SQLite Storage And Matching

**Files:**
- Create: `internal/db/worktree_mappings.go`
- Modify: `internal/db/schema.sql`
- Modify: `internal/db/db.go`
- Test: `internal/db/worktree_mappings_test.go`

- [ ] **Step 1: Write failing DB tests**

Create `internal/db/worktree_mappings_test.go`:

```go
package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestWorktreeProjectMappingsCRUDNormalizesAndScopesByMachine(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()

	prefix := filepath.Join(t.TempDir(), "my-app.worktrees")
	m, err := d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine:    "laptop",
		PathPrefix: prefix + string(filepath.Separator),
		Project:    "my-app",
		Enabled:    true,
	})
	requireNoError(t, err, "create mapping")
	if m.Machine != "laptop" {
		t.Fatalf("machine = %q, want laptop", m.Machine)
	}
	if m.PathPrefix != prefix {
		t.Fatalf("path_prefix = %q, want %q", m.PathPrefix, prefix)
	}
	if m.Project != "my_app" {
		t.Fatalf("project = %q, want my_app", m.Project)
	}

	got, err := d.ListWorktreeProjectMappings(ctx, "laptop")
	requireNoError(t, err, "list laptop mappings")
	if len(got) != 1 || got[0].ID != m.ID {
		t.Fatalf("laptop mappings = %+v, want created mapping", got)
	}

	other, err := d.ListWorktreeProjectMappings(ctx, "server")
	requireNoError(t, err, "list server mappings")
	if len(other) != 0 {
		t.Fatalf("server mappings = %+v, want none", other)
	}
}

func TestWorktreeProjectMappingsRejectInvalidAndDuplicateRows(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	prefix := filepath.Join(t.TempDir(), "repo.worktrees")

	_, err := d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: " ", Project: "repo", Enabled: true,
	})
	if err == nil {
		t.Fatal("empty path prefix accepted")
	}
	_, err = d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: prefix, Project: " ", Enabled: true,
	})
	if err == nil {
		t.Fatal("empty project accepted")
	}

	_, err = d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: prefix, Project: "repo", Enabled: true,
	})
	requireNoError(t, err, "create first mapping")
	_, err = d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: prefix, Project: "repo2", Enabled: true,
	})
	if !errors.Is(err, ErrWorktreeMappingDuplicate) {
		t.Fatalf("duplicate error = %v, want ErrWorktreeMappingDuplicate", err)
	}
}

func TestResolveWorktreeProjectMappingUsesLongestPrefixAndBoundaries(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	root := t.TempDir()
	broad := filepath.Join(root, "repo.worktrees")
	nested := filepath.Join(broad, "special")

	_, err := d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: broad, Project: "repo", Enabled: true,
	})
	requireNoError(t, err, "create broad mapping")
	_, err = d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: nested, Project: "special-repo", Enabled: true,
	})
	requireNoError(t, err, "create nested mapping")

	project, ok, err := d.ResolveWorktreeProjectMapping(ctx, "laptop",
		filepath.Join(nested, "feat", "thing"), "leaf")
	requireNoError(t, err, "resolve nested")
	if !ok || project != "special_repo" {
		t.Fatalf("nested resolve = (%q,%v), want (special_repo,true)", project, ok)
	}

	project, ok, err = d.ResolveWorktreeProjectMapping(ctx, "laptop",
		filepath.Join(broad, "feat", "thing"), "leaf")
	requireNoError(t, err, "resolve broad")
	if !ok || project != "repo" {
		t.Fatalf("broad resolve = (%q,%v), want (repo,true)", project, ok)
	}

	_, ok, err = d.ResolveWorktreeProjectMapping(ctx, "laptop", broad+"-other", "leaf")
	requireNoError(t, err, "resolve boundary miss")
	if ok {
		t.Fatal("path with shared string prefix matched across component boundary")
	}
}

func TestApplyWorktreeProjectMappingsUpdatesOnlyCurrentMachineAndEnabledRows(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	root := t.TempDir()
	prefix := filepath.Join(root, "repo.worktrees")
	disabledPrefix := filepath.Join(root, "disabled.worktrees")

	_, err := d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: prefix, Project: "repo", Enabled: true,
	})
	requireNoError(t, err, "create enabled mapping")
	_, err = d.CreateWorktreeProjectMapping(ctx, WorktreeProjectMapping{
		Machine: "laptop", PathPrefix: disabledPrefix, Project: "disabled", Enabled: false,
	})
	requireNoError(t, err, "create disabled mapping")

	insert := func(id, machine, project, cwd string) {
		t.Helper()
		err := d.UpsertSession(Session{
			ID: id, Project: project, Machine: machine, Agent: "claude", Cwd: cwd,
		})
		requireNoError(t, err, "insert "+id)
	}
	insert("match", "laptop", "leaf", filepath.Join(prefix, "feat", "thing"))
	insert("same-project", "laptop", "repo", filepath.Join(prefix, "bugfix"))
	insert("other-machine", "server", "leaf", filepath.Join(prefix, "feat", "thing"))
	insert("disabled", "laptop", "leaf", filepath.Join(disabledPrefix, "feat"))

	result, err := d.ApplyWorktreeProjectMappings(ctx, "laptop")
	requireNoError(t, err, "apply mappings")
	if result.MatchedSessions != 2 || result.UpdatedSessions != 1 {
		t.Fatalf("apply result = %+v, want matched=2 updated=1", result)
	}
	assertSessionProject(t, d, "match", "repo")
	assertSessionProject(t, d, "same-project", "repo")
	assertSessionProject(t, d, "other-machine", "leaf")
	assertSessionProject(t, d, "disabled", "leaf")
}

func assertSessionProject(t *testing.T, d *DB, id, want string) {
	t.Helper()
	got, err := d.GetSession(context.Background(), id)
	requireNoError(t, err, "GetSession "+id)
	if got.Project != want {
		t.Fatalf("session %s project = %q, want %q", id, got.Project, want)
	}
}
```

- [ ] **Step 2: Run DB tests to verify failure**

Run:

```bash
go test ./internal/db -run 'TestWorktreeProjectMappings|TestResolveWorktreeProjectMapping|TestApplyWorktreeProjectMappings' -count=1
```

Expected: FAIL because `WorktreeProjectMapping`, `CreateWorktreeProjectMapping`, `ResolveWorktreeProjectMapping`, and related methods do not exist.

- [ ] **Step 3: Add schema DDL**

Append this to `internal/db/schema.sql` near the other local settings tables:

```sql
CREATE TABLE IF NOT EXISTS worktree_project_mappings (
    id          INTEGER PRIMARY KEY,
    machine     TEXT NOT NULL,
    path_prefix TEXT NOT NULL,
    project     TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(machine, path_prefix)
);

CREATE INDEX IF NOT EXISTS idx_worktree_project_mappings_match
    ON worktree_project_mappings(machine, enabled, path_prefix);

CREATE INDEX IF NOT EXISTS idx_worktree_project_mappings_project
    ON worktree_project_mappings(machine, project);
```

In `internal/db/db.go`, add this inside `migrateColumns` after `remote_skipped_files` creation:

```go
	if _, err := w.Exec(`
		CREATE TABLE IF NOT EXISTS worktree_project_mappings (
			id          INTEGER PRIMARY KEY,
			machine     TEXT NOT NULL,
			path_prefix TEXT NOT NULL,
			project     TEXT NOT NULL,
			enabled     INTEGER NOT NULL DEFAULT 1,
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			UNIQUE(machine, path_prefix)
		);
		CREATE INDEX IF NOT EXISTS idx_worktree_project_mappings_match
			ON worktree_project_mappings(machine, enabled, path_prefix);
		CREATE INDEX IF NOT EXISTS idx_worktree_project_mappings_project
			ON worktree_project_mappings(machine, project);
	`); err != nil {
		return fmt.Errorf("creating worktree_project_mappings: %w", err)
	}
```

- [ ] **Step 4: Implement DB mapping methods**

Create `internal/db/worktree_mappings.go`:

```go
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mattn/go-sqlite3"
	"github.com/wesm/agentsview/internal/parser"
)

var ErrWorktreeMappingDuplicate = errors.New("worktree mapping already exists")

type WorktreeProjectMapping struct {
	ID         int64  `json:"id"`
	Machine    string `json:"machine"`
	PathPrefix string `json:"path_prefix"`
	Project    string `json:"project"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type ApplyWorktreeProjectMappingsResult struct {
	MatchedSessions int `json:"matched_sessions"`
	UpdatedSessions int `json:"updated_sessions"`
}

func normalizeWorktreeMapping(machine, pathPrefix, project string) (WorktreeProjectMapping, error) {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("machine is required")
	}
	pathPrefix = strings.TrimSpace(pathPrefix)
	if pathPrefix == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("path_prefix is required")
	}
	clean := filepath.Clean(pathPrefix)
	if clean == "." {
		return WorktreeProjectMapping{}, fmt.Errorf("path_prefix is required")
	}
	if vol := filepath.VolumeName(clean); clean != vol+string(filepath.Separator) {
		clean = strings.TrimRight(clean, string(filepath.Separator))
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("project is required")
	}
	project = parser.NormalizeName(project)
	return WorktreeProjectMapping{Machine: machine, PathPrefix: clean, Project: project}, nil
}

func worktreePathMatches(prefix, cwd string) bool {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return false
	}
	clean := filepath.Clean(cwd)
	if clean == prefix {
		return true
	}
	p := strings.TrimRight(prefix, string(filepath.Separator)) + string(filepath.Separator)
	return strings.HasPrefix(clean, p)
}

func scanWorktreeMapping(rows *sql.Rows) (WorktreeProjectMapping, error) {
	var m WorktreeProjectMapping
	var enabled int
	if err := rows.Scan(&m.ID, &m.Machine, &m.PathPrefix, &m.Project, &enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return m, err
	}
	m.Enabled = enabled != 0
	return m, nil
}

func (db *DB) ListWorktreeProjectMappings(ctx context.Context, machine string) ([]WorktreeProjectMapping, error) {
	rows, err := db.getReader().QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ?
		ORDER BY path_prefix`, machine)
	if err != nil {
		return nil, fmt.Errorf("listing worktree mappings: %w", err)
	}
	defer rows.Close()
	var out []WorktreeProjectMapping
	for rows.Next() {
		m, err := scanWorktreeMapping(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning worktree mapping: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) CreateWorktreeProjectMapping(ctx context.Context, m WorktreeProjectMapping) (WorktreeProjectMapping, error) {
	n, err := normalizeWorktreeMapping(m.Machine, m.PathPrefix, m.Project)
	if err != nil {
		return WorktreeProjectMapping{}, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	res, err := db.getWriter().ExecContext(ctx, `
		INSERT INTO worktree_project_mappings (machine, path_prefix, project, enabled)
		VALUES (?, ?, ?, ?)`, n.Machine, n.PathPrefix, n.Project, enabled)
	if err != nil {
		if isSQLiteUniqueConstraint(err) {
			return WorktreeProjectMapping{}, ErrWorktreeMappingDuplicate
		}
		return WorktreeProjectMapping{}, fmt.Errorf("creating worktree mapping: %w", err)
	}
	n.ID, _ = res.LastInsertId()
	n.Enabled = m.Enabled
	return db.getWorktreeProjectMappingLocked(ctx, n.Machine, n.ID)
}

func (db *DB) UpdateWorktreeProjectMapping(ctx context.Context, machine string, id int64, patch WorktreeProjectMapping) (WorktreeProjectMapping, error) {
	n, err := normalizeWorktreeMapping(machine, patch.PathPrefix, patch.Project)
	if err != nil {
		return WorktreeProjectMapping{}, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	enabled := 0
	if patch.Enabled {
		enabled = 1
	}
	res, err := db.getWriter().ExecContext(ctx, `
		UPDATE worktree_project_mappings
		SET path_prefix = ?, project = ?, enabled = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND machine = ?`, n.PathPrefix, n.Project, enabled, id, machine)
	if err != nil {
		if isSQLiteUniqueConstraint(err) {
			return WorktreeProjectMapping{}, ErrWorktreeMappingDuplicate
		}
		return WorktreeProjectMapping{}, fmt.Errorf("updating worktree mapping: %w", err)
	}
	changed, _ := res.RowsAffected()
	if changed == 0 {
		return WorktreeProjectMapping{}, sql.ErrNoRows
	}
	return db.getWorktreeProjectMappingLocked(ctx, machine, id)
}

func (db *DB) DeleteWorktreeProjectMapping(ctx context.Context, machine string, id int64) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	res, err := db.getWriter().ExecContext(ctx,
		`DELETE FROM worktree_project_mappings WHERE id = ? AND machine = ?`, id, machine)
	if err != nil {
		return fmt.Errorf("deleting worktree mapping: %w", err)
	}
	changed, _ := res.RowsAffected()
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) getWorktreeProjectMappingLocked(ctx context.Context, machine string, id int64) (WorktreeProjectMapping, error) {
	row := db.getWriter().QueryRowContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE id = ? AND machine = ?`, id, machine)
	var m WorktreeProjectMapping
	var enabled int
	if err := row.Scan(&m.ID, &m.Machine, &m.PathPrefix, &m.Project, &enabled, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return m, err
	}
	m.Enabled = enabled != 0
	return m, nil
}

func (db *DB) ResolveWorktreeProjectMapping(ctx context.Context, machine, cwd, currentProject string) (string, bool, error) {
	matches, err := db.activeWorktreeProjectMappings(ctx, machine)
	if err != nil {
		return currentProject, false, err
	}
	if m, ok := bestWorktreeProjectMapping(matches, cwd); ok {
		return m.Project, true, nil
	}
	return currentProject, false, nil
}

func (db *DB) activeWorktreeProjectMappings(ctx context.Context, machine string) ([]WorktreeProjectMapping, error) {
	rows, err := db.getReader().QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ? AND enabled = 1`, machine)
	if err != nil {
		return nil, fmt.Errorf("querying active worktree mappings: %w", err)
	}
	defer rows.Close()
	var out []WorktreeProjectMapping
	for rows.Next() {
		m, err := scanWorktreeMapping(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func bestWorktreeProjectMapping(mappings []WorktreeProjectMapping, cwd string) (WorktreeProjectMapping, bool) {
	sort.SliceStable(mappings, func(i, j int) bool {
		return len(mappings[i].PathPrefix) > len(mappings[j].PathPrefix)
	})
	for _, m := range mappings {
		if worktreePathMatches(m.PathPrefix, cwd) {
			return m, true
		}
	}
	return WorktreeProjectMapping{}, false
}

func (db *DB) ApplyWorktreeProjectMappings(ctx context.Context, machine string) (ApplyWorktreeProjectMappingsResult, error) {
	mappings, err := db.activeWorktreeProjectMappings(ctx, machine)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, err
	}
	rows, err := db.getReader().QueryContext(ctx,
		`SELECT id, project, cwd FROM sessions WHERE machine = ? AND cwd != '' AND deleted_at IS NULL`, machine)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf("querying sessions for mapping apply: %w", err)
	}
	type update struct{ id, project string }
	var updates []update
	var result ApplyWorktreeProjectMappingsResult
	for rows.Next() {
		var id, project, cwd string
		if err := rows.Scan(&id, &project, &cwd); err != nil {
			rows.Close()
			return result, err
		}
		if m, ok := bestWorktreeProjectMapping(mappings, cwd); ok {
			result.MatchedSessions++
			if project != m.Project {
				updates = append(updates, update{id: id, project: m.Project})
			}
		}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	tx, err := db.getWriter().BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET project = ? WHERE id = ?`, u.project, u.id); err != nil {
			return result, fmt.Errorf("applying worktree mapping to %s: %w", u.id, err)
		}
		result.UpdatedSessions++
	}
	return result, tx.Commit()
}

func isSQLiteUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}
```

- [ ] **Step 5: Run DB tests to verify pass**

Run:

```bash
go test ./internal/db -run 'TestWorktreeProjectMappings|TestResolveWorktreeProjectMapping|TestApplyWorktreeProjectMappings' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit DB storage**

```bash
git add internal/db/schema.sql internal/db/db.go internal/db/worktree_mappings.go internal/db/worktree_mappings_test.go
git commit -m "feat(db): add worktree project mappings"
```

## Task 2: Apply Mappings During Sync

**Files:**
- Modify: `internal/sync/engine.go`
- Test: `internal/sync/engine_integration_test.go`

- [ ] **Step 1: Write failing sync integration test**

Add to `internal/sync/engine_integration_test.go`:

```go
func TestSyncEngineAppliesWorktreeProjectMapping(t *testing.T) {
	env := setupSyncTest(t)
	ctx := context.Background()
	prefix := filepath.Join(t.TempDir(), "agentsview.worktrees")
	_, err := env.db.CreateWorktreeProjectMapping(ctx, db.WorktreeProjectMapping{
		Machine:    "local",
		PathPrefix: prefix,
		Project:    "agentsview",
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("CreateWorktreeProjectMapping: %v", err)
	}

	cwd := filepath.Join(prefix, "feat", "my-new-feature")
	content := testjsonl.NewSessionBuilder().
		AddClaudeUser(tsEarly, "hello", cwd).
		AddClaudeAssistant(tsLate, "hi").
		String()
	writeClaudeSession(t, env.claudeDir, "-Users-me-code-agentsview-worktrees-feat-my-new-feature", "mapped.jsonl", content)

	if err := env.engine.SyncAll(context.Background()); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	assertSessionProject(t, env.db, "mapped", "agentsview")
}
```

- [ ] **Step 2: Run test to verify failure**

Run:

```bash
go test ./internal/sync -run TestSyncEngineAppliesWorktreeProjectMapping -count=1
```

Expected: FAIL because sync does not consult mappings yet.

- [ ] **Step 3: Add engine machine accessor and mapping application**

In `internal/sync/engine.go`, add after `NewEngine`:

```go
// Machine returns the machine name assigned to sessions written by this engine.
func (e *Engine) Machine() string {
	if e == nil {
		return ""
	}
	return e.machine
}
```

In `prepareSessionWrite`, after `e.applyRemoteRewrites(&s, msgs)` and before `s.IsAutomated = ...`, add:

```go
	if s.Cwd != "" {
		if mapped, ok, err := e.db.ResolveWorktreeProjectMapping(
			context.Background(), s.Machine, s.Cwd, s.Project,
		); err != nil {
			log.Printf("resolve worktree project mapping for %s: %v", s.ID, err)
		} else if ok {
			s.Project = mapped
		}
	}
```

- [ ] **Step 4: Run sync test to verify pass**

Run:

```bash
go test ./internal/sync -run TestSyncEngineAppliesWorktreeProjectMapping -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit sync application**

```bash
git add internal/sync/engine.go internal/sync/engine_integration_test.go
git commit -m "feat(sync): apply worktree project mappings"
```

## Task 3: HTTP API

**Files:**
- Create: `internal/server/worktree_mappings.go`
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write failing server tests**

Add to `internal/server/server_test.go`:

```go
func TestWorktreeMappingsAPIUsesCurrentMachine(t *testing.T) {
	srv := testServer(t, 30*time.Second)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"machine":"other","path_prefix":"/tmp/repo.worktrees/","project":"repo-name","enabled":true}`
	resp, err := http.Post(ts.URL+"/api/v1/settings/worktree-mappings", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST mapping: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d", resp.StatusCode)
	}

	var created db.WorktreeProjectMapping
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.Machine != "test" {
		t.Fatalf("machine = %q, want test", created.Machine)
	}
	if created.Project != "repo_name" {
		t.Fatalf("project = %q, want repo_name", created.Project)
	}

	resp, err = http.Get(ts.URL + "/api/v1/settings/worktree-mappings")
	if err != nil {
		t.Fatalf("GET mappings: %v", err)
	}
	defer resp.Body.Close()
	var list struct {
		Machine  string                      `json:"machine"`
		Mappings []db.WorktreeProjectMapping `json:"mappings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Machine != "test" || len(list.Mappings) != 1 {
		t.Fatalf("list = %+v, want machine test and one mapping", list)
	}
}

func TestWorktreeMappingsApplyAPI(t *testing.T) {
	srv := testServer(t, 30*time.Second)
	local := srv.db.(*db.DB)
	prefix := filepath.Join(t.TempDir(), "repo.worktrees")
	_, err := local.CreateWorktreeProjectMapping(context.Background(), db.WorktreeProjectMapping{
		Machine: "test", PathPrefix: prefix, Project: "repo", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktreeProjectMapping: %v", err)
	}
	if err := local.UpsertSession(db.Session{
		ID: "s1", Project: "leaf", Machine: "test", Agent: "claude", Cwd: filepath.Join(prefix, "feat", "x"),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/v1/settings/worktree-mappings/apply", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("POST apply: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply status = %d", resp.StatusCode)
	}
	var result db.ApplyWorktreeProjectMappingsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode apply: %v", err)
	}
	if result.MatchedSessions != 1 || result.UpdatedSessions != 1 {
		t.Fatalf("apply result = %+v, want matched=1 updated=1", result)
	}
	got, err := local.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Project != "repo" {
		t.Fatalf("project = %q, want repo", got.Project)
	}
}
```

- [ ] **Step 2: Run server tests to verify failure**

Run:

```bash
go test ./internal/server -run 'TestWorktreeMappingsAPI|TestWorktreeMappingsApplyAPI' -count=1
```

Expected: FAIL with 404 routes.

- [ ] **Step 3: Implement server handlers**

Create `internal/server/worktree_mappings.go`:

```go
package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/wesm/agentsview/internal/db"
)

type worktreeMappingsResponse struct {
	Machine  string                      `json:"machine"`
	Mappings []db.WorktreeProjectMapping `json:"mappings"`
}

func (s *Server) currentMachine() string {
	if s.engine != nil && s.engine.Machine() != "" {
		return s.engine.Machine()
	}
	return "local"
}

func (s *Server) localDBForWorktreeMappings(w http.ResponseWriter) (*db.DB, bool) {
	if s.db.ReadOnly() {
		writeError(w, http.StatusNotImplemented, "settings cannot be modified in read-only mode")
		return nil, false
	}
	local, ok := s.db.(*db.DB)
	if !ok {
		writeError(w, http.StatusNotImplemented, "settings cannot be modified in read-only mode")
		return nil, false
	}
	return local, true
}

func (s *Server) handleListWorktreeMappings(w http.ResponseWriter, r *http.Request) {
	local, ok := s.localDBForWorktreeMappings(w)
	if !ok {
		return
	}
	machine := s.currentMachine()
	mappings, err := local.ListWorktreeProjectMappings(r.Context(), machine)
	if err != nil {
		log.Printf("list worktree mappings: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, worktreeMappingsResponse{Machine: machine, Mappings: mappings})
}

func (s *Server) handleCreateWorktreeMapping(w http.ResponseWriter, r *http.Request) {
	local, ok := s.localDBForWorktreeMappings(w)
	if !ok {
		return
	}
	var req db.WorktreeProjectMapping
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.Machine = s.currentMachine()
	m, err := local.CreateWorktreeProjectMapping(r.Context(), req)
	if err != nil {
		handleWorktreeMappingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) handleUpdateWorktreeMapping(w http.ResponseWriter, r *http.Request) {
	local, ok := s.localDBForWorktreeMappings(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid mapping id")
		return
	}
	var req db.WorktreeProjectMapping
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m, err := local.UpdateWorktreeProjectMapping(r.Context(), s.currentMachine(), id, req)
	if err != nil {
		handleWorktreeMappingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteWorktreeMapping(w http.ResponseWriter, r *http.Request) {
	local, ok := s.localDBForWorktreeMappings(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid mapping id")
		return
	}
	if err := local.DeleteWorktreeProjectMapping(r.Context(), s.currentMachine(), id); err != nil {
		handleWorktreeMappingError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleApplyWorktreeMappings(w http.ResponseWriter, r *http.Request) {
	local, ok := s.localDBForWorktreeMappings(w)
	if !ok {
		return
	}
	result, err := local.ApplyWorktreeProjectMappings(r.Context(), s.currentMachine())
	if err != nil {
		log.Printf("apply worktree mappings: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func handleWorktreeMappingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "mapping not found")
	case errors.Is(err, db.ErrWorktreeMappingDuplicate):
		writeError(w, http.StatusConflict, "mapping already exists")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
```

In `internal/server/server.go`, register:

```go
	s.mux.Handle("GET /api/v1/settings/worktree-mappings", s.withTimeout(s.handleListWorktreeMappings))
	s.mux.Handle("POST /api/v1/settings/worktree-mappings", s.withTimeout(s.handleCreateWorktreeMapping))
	s.mux.Handle("PUT /api/v1/settings/worktree-mappings/{id}", s.withTimeout(s.handleUpdateWorktreeMapping))
	s.mux.Handle("DELETE /api/v1/settings/worktree-mappings/{id}", s.withTimeout(s.handleDeleteWorktreeMapping))
	s.mux.Handle("POST /api/v1/settings/worktree-mappings/apply", s.withTimeout(s.handleApplyWorktreeMappings))
```

- [ ] **Step 4: Run server tests to verify pass**

Run:

```bash
go test ./internal/server -run 'TestWorktreeMappingsAPI|TestWorktreeMappingsApplyAPI' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit API**

```bash
git add internal/server/server.go internal/server/worktree_mappings.go internal/server/server_test.go internal/sync/engine.go
git commit -m "feat(server): expose worktree mapping settings"
```

## Task 4: Frontend API And Settings Panel

**Files:**
- Modify: `frontend/src/lib/api/client.ts`
- Create: `frontend/src/lib/components/settings/WorktreeMappingsSettings.svelte`
- Modify: `frontend/src/lib/components/settings/SettingsPage.svelte`
- Test: `frontend/src/lib/components/settings/WorktreeMappingsSettings.test.ts`

- [ ] **Step 1: Add frontend API types**

In `frontend/src/lib/api/client.ts`, add near Settings:

```ts
export interface WorktreeProjectMapping {
  id: number;
  machine: string;
  path_prefix: string;
  project: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface WorktreeMappingsResponse {
  machine: string;
  mappings: WorktreeProjectMapping[];
}

export interface WorktreeMappingInput {
  path_prefix: string;
  project: string;
  enabled: boolean;
}

export interface ApplyWorktreeMappingsResult {
  matched_sessions: number;
  updated_sessions: number;
}

export function listWorktreeMappings(): Promise<WorktreeMappingsResponse> {
  return fetchJSON("/settings/worktree-mappings");
}

export function createWorktreeMapping(
  input: WorktreeMappingInput,
): Promise<WorktreeProjectMapping> {
  return fetchJSON("/settings/worktree-mappings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export function updateWorktreeMapping(
  id: number,
  input: WorktreeMappingInput,
): Promise<WorktreeProjectMapping> {
  return fetchJSON(`/settings/worktree-mappings/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
}

export async function deleteWorktreeMapping(id: number): Promise<void> {
  const res = await fetch(`${getBase()}/settings/worktree-mappings/${id}`, authHeaders({
    method: "DELETE",
  }));
  if (!res.ok) {
    const body = await res.text();
    throw new ApiError(res.status, apiErrorMessage(res.status, body));
  }
}

export function applyWorktreeMappings(): Promise<ApplyWorktreeMappingsResult> {
  return fetchJSON("/settings/worktree-mappings/apply", { method: "POST" });
}
```

- [ ] **Step 2: Create settings component**

Create `frontend/src/lib/components/settings/WorktreeMappingsSettings.svelte`:

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import SettingsSection from "./SettingsSection.svelte";
  import {
    applyWorktreeMappings,
    createWorktreeMapping,
    deleteWorktreeMapping,
    listWorktreeMappings,
    updateWorktreeMapping,
    type WorktreeProjectMapping,
  } from "../../api/client.js";

  type EditableMapping = WorktreeProjectMapping & { dirty?: boolean; pending?: boolean };

  let machine = $state("");
  let mappings: EditableMapping[] = $state([]);
  let pathPrefix = $state("");
  let project = $state("");
  let enabled = $state(true);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let applyResult = $state<string | null>(null);

  onMount(loadMappings);

  async function loadMappings() {
    loading = true;
    error = null;
    try {
      const data = await listWorktreeMappings();
      machine = data.machine;
      mappings = data.mappings.map((m) => ({ ...m, dirty: false, pending: false }));
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load worktree mappings";
    } finally {
      loading = false;
    }
  }

  async function addMapping() {
    if (!pathPrefix.trim() || !project.trim()) return;
    error = null;
    try {
      await createWorktreeMapping({
        path_prefix: pathPrefix,
        project,
        enabled,
      });
      pathPrefix = "";
      project = "";
      enabled = true;
      await loadMappings();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to add mapping";
    }
  }

  async function saveMapping(mapping: EditableMapping) {
    mapping.pending = true;
    error = null;
    try {
      await updateWorktreeMapping(mapping.id, {
        path_prefix: mapping.path_prefix,
        project: mapping.project,
        enabled: mapping.enabled,
      });
      await loadMappings();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to save mapping";
    } finally {
      mapping.pending = false;
    }
  }

  async function removeMapping(mapping: EditableMapping) {
    mapping.pending = true;
    error = null;
    try {
      await deleteWorktreeMapping(mapping.id);
      await loadMappings();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to delete mapping";
    }
  }

  async function applyMappings() {
    error = null;
    applyResult = null;
    try {
      const result = await applyWorktreeMappings();
      applyResult = `${result.updated_sessions} updated, ${result.matched_sessions} matched`;
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to apply mappings";
    }
  }
</script>

<SettingsSection
  title="Worktree Mappings"
  description="Map local worktree path prefixes to project names for this machine."
>
  <div class="machine-row">
    <span class="label">Machine</span>
    <code>{machine || "local"}</code>
  </div>

  {#if loading}
    <div class="muted">Loading mappings...</div>
  {:else}
    <div class="mapping-list">
      {#each mappings as mapping}
        <div class="mapping-row">
          <input class="path-input" bind:value={mapping.path_prefix} oninput={() => (mapping.dirty = true)} />
          <input class="project-input" bind:value={mapping.project} oninput={() => (mapping.dirty = true)} />
          <label class="toggle">
            <input type="checkbox" bind:checked={mapping.enabled} onchange={() => (mapping.dirty = true)} />
            Enabled
          </label>
          <button disabled={!mapping.dirty || mapping.pending} onclick={() => saveMapping(mapping)}>Save</button>
          <button disabled={mapping.pending} onclick={() => removeMapping(mapping)}>Delete</button>
        </div>
      {/each}
    </div>

    <div class="add-row">
      <input class="path-input" placeholder="/Users/me/code/repo.worktrees" bind:value={pathPrefix} />
      <input class="project-input" placeholder="repo" bind:value={project} />
      <label class="toggle">
        <input type="checkbox" bind:checked={enabled} />
        Enabled
      </label>
      <button disabled={!pathPrefix.trim() || !project.trim()} onclick={addMapping}>Add</button>
    </div>
  {/if}

  <div class="apply-row">
    <button onclick={applyMappings}>Apply mappings</button>
    {#if applyResult}
      <span class="muted">{applyResult}</span>
    {/if}
  </div>

  {#if error}
    <div class="error">{error}</div>
  {/if}
</SettingsSection>

<style>
  .machine-row,
  .mapping-row,
  .add-row,
  .apply-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .mapping-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .label {
    min-width: 72px;
    font-size: 12px;
    color: var(--text-secondary);
  }
  .path-input {
    flex: 2;
    min-width: 0;
  }
  .project-input {
    flex: 1;
    min-width: 0;
  }
  input {
    height: 30px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    font-size: 12px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
  }
  button {
    height: 28px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    font-size: 12px;
    color: var(--text-primary);
    background: var(--bg-inset);
    border: 1px solid var(--border-muted);
    cursor: pointer;
    white-space: nowrap;
  }
  button:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .toggle,
  .muted {
    font-size: 11px;
    color: var(--text-muted);
  }
  .error {
    font-size: 12px;
    color: var(--accent-red, #ef4444);
  }
</style>
```

- [ ] **Step 3: Render the panel**

Modify `frontend/src/lib/components/settings/SettingsPage.svelte`:

```svelte
  import WorktreeMappingsSettings from "./WorktreeMappingsSettings.svelte";
```

Render it after `AgentDirSettings`:

```svelte
      <AgentDirSettings />
      <WorktreeMappingsSettings />
      <TerminalSettings />
```

- [ ] **Step 4: Add focused frontend test**

Create `frontend/src/lib/components/settings/WorktreeMappingsSettings.test.ts`:

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, unmount, tick } from "svelte";

const api = vi.hoisted(() => ({
  listWorktreeMappings: vi.fn(),
  createWorktreeMapping: vi.fn(),
  updateWorktreeMapping: vi.fn(),
  deleteWorktreeMapping: vi.fn(),
  applyWorktreeMappings: vi.fn(),
}));

vi.mock("../../api/client.js", () => api);

// @ts-ignore
import WorktreeMappingsSettings from "./WorktreeMappingsSettings.svelte";

async function flush() {
  for (let i = 0; i < 5; i++) {
    await tick();
  }
}

function inputByPlaceholder(placeholder: string): HTMLInputElement {
  const input = document.querySelector<HTMLInputElement>(
    `input[placeholder="${placeholder}"]`,
  );
  if (!input) throw new Error(`input ${placeholder} not found`);
  return input;
}

async function setInput(input: HTMLInputElement, value: string) {
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  await tick();
}

function clickButton(text: string) {
  const button = Array.from(document.querySelectorAll("button")).find(
    (el) => el.textContent?.trim() === text,
  ) as HTMLButtonElement | undefined;
  if (!button) throw new Error(`button ${text} not found`);
  button.click();
}

describe("WorktreeMappingsSettings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = "";
    api.listWorktreeMappings.mockResolvedValue({
      machine: "laptop",
      mappings: [],
    });
  });

  it("shows current machine without a machine input", async () => {
    const component = mount(WorktreeMappingsSettings, {
      target: document.body,
    });
    await flush();

    expect(document.body.textContent).toContain("laptop");
    expect(document.querySelector('input[placeholder="laptop"]')).toBeNull();

    unmount(component);
  });

  it("adds and applies mappings", async () => {
    api.createWorktreeMapping.mockResolvedValue({
      id: 1,
      machine: "laptop",
      path_prefix: "/tmp/repo.worktrees",
      project: "repo",
      enabled: true,
      created_at: "",
      updated_at: "",
    });
    api.applyWorktreeMappings.mockResolvedValue({
      matched_sessions: 2,
      updated_sessions: 1,
    });

    const component = mount(WorktreeMappingsSettings, {
      target: document.body,
    });
    await flush();

    await setInput(
      inputByPlaceholder("/Users/me/code/repo.worktrees"),
      "/tmp/repo.worktrees",
    );
    await setInput(inputByPlaceholder("repo"), "repo");
    clickButton("Add");
    await flush();

    expect(api.createWorktreeMapping).toHaveBeenCalledWith({
      path_prefix: "/tmp/repo.worktrees",
      project: "repo",
      enabled: true,
    });

    clickButton("Apply mappings");
    await flush();

    expect(document.body.textContent).toContain("1 updated, 2 matched");
    unmount(component);
  });
});
```

- [ ] **Step 5: Run frontend checks**

Run:

```bash
bun run --cwd frontend test -- WorktreeMappingsSettings.test.ts
bun run --cwd frontend check
```

Expected: both commands pass.

- [ ] **Step 6: Commit frontend**

```bash
git add frontend/src/lib/api/client.ts frontend/src/lib/components/settings/SettingsPage.svelte frontend/src/lib/components/settings/WorktreeMappingsSettings.svelte frontend/src/lib/components/settings/WorktreeMappingsSettings.test.ts
git commit -m "feat(frontend): manage worktree mappings"
```

## Task 5: Final Verification

**Files:**
- Review all modified files from Tasks 1-4.

- [ ] **Step 1: Run focused backend tests**

Run:

```bash
go test ./internal/db ./internal/server ./internal/sync -run 'Worktree|WorktreeMappings|TestSyncEngineAppliesWorktreeProjectMapping' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend packages touched**

Run:

```bash
go test ./internal/db ./internal/server ./internal/sync
```

Expected: PASS.

- [ ] **Step 3: Run frontend validation**

Run:

```bash
bun run --cwd frontend check
bun run --cwd frontend test -- WorktreeMappingsSettings.test.ts
```

Expected: PASS.

- [ ] **Step 4: Inspect final diff**

Run:

```bash
git status --short
git diff --stat origin/main...HEAD
```

Expected: no uncommitted changes except intentionally pending files before the final commit.

- [ ] **Step 5: Commit any final fixes**

If final verification required changes, stage only the specific files changed by those fixes. For example, if the frontend component and DB mapping file changed during verification:

```bash
git add frontend/src/lib/components/settings/WorktreeMappingsSettings.svelte internal/db/worktree_mappings.go
git commit -m "fix: polish worktree mapping implementation"
```

If there are no changes, do not create an empty commit.

## Self-Review Checklist

- Spec coverage: DB mappings, implicit current-machine scoping, no machine selector, manual-only UI, explicit apply, Full Resync compatibility through sync-time mapping, and no `{project}.worktrees` parser heuristic are all covered.
- Placeholder scan: no implementation step relies on "TODO", "TBD", or unstated behavior.
- Type consistency: `WorktreeProjectMapping`, `ApplyWorktreeProjectMappingsResult`, endpoint paths, and frontend type names are consistent across tasks.
