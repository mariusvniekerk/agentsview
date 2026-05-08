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

func normalizeWorktreeMapping(
	machine string,
	pathPrefix string,
	project string,
) (WorktreeProjectMapping, error) {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("machine is required")
	}

	pathPrefix = strings.TrimSpace(pathPrefix)
	if pathPrefix == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("path_prefix is required")
	}
	cleanPrefix := filepath.Clean(pathPrefix)
	if cleanPrefix == "." {
		return WorktreeProjectMapping{}, fmt.Errorf("path_prefix is required")
	}
	if !isFilesystemRoot(cleanPrefix) {
		cleanPrefix = strings.TrimRight(cleanPrefix, string(filepath.Separator))
	}

	project = strings.TrimSpace(project)
	if project == "" {
		return WorktreeProjectMapping{}, fmt.Errorf("project is required")
	}

	return WorktreeProjectMapping{
		Machine:    machine,
		PathPrefix: cleanPrefix,
		Project:    parser.NormalizeName(project),
	}, nil
}

func isFilesystemRoot(path string) bool {
	volume := filepath.VolumeName(path)
	return path == volume+string(filepath.Separator)
}

func worktreePathMatches(prefix string, cwd string) bool {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return false
	}
	cleanCwd := filepath.Clean(cwd)
	if cleanCwd == prefix {
		return true
	}
	matchPrefix := prefix
	if !isFilesystemRoot(matchPrefix) {
		matchPrefix = strings.TrimRight(matchPrefix, string(filepath.Separator))
		matchPrefix += string(filepath.Separator)
	}
	return strings.HasPrefix(cleanCwd, matchPrefix)
}

func scanWorktreeMapping(rows *sql.Rows) (WorktreeProjectMapping, error) {
	var m WorktreeProjectMapping
	var enabled int
	if err := rows.Scan(
		&m.ID,
		&m.Machine,
		&m.PathPrefix,
		&m.Project,
		&enabled,
		&m.CreatedAt,
		&m.UpdatedAt,
	); err != nil {
		return m, err
	}
	m.Enabled = enabled != 0
	return m, nil
}

func scanWorktreeMappingRow(row *sql.Row) (WorktreeProjectMapping, error) {
	var m WorktreeProjectMapping
	var enabled int
	if err := row.Scan(
		&m.ID,
		&m.Machine,
		&m.PathPrefix,
		&m.Project,
		&enabled,
		&m.CreatedAt,
		&m.UpdatedAt,
	); err != nil {
		return m, err
	}
	m.Enabled = enabled != 0
	return m, nil
}

func (db *DB) ListWorktreeProjectMappings(
	ctx context.Context,
	machine string,
) ([]WorktreeProjectMapping, error) {
	rows, err := db.getReader().QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ?
		ORDER BY path_prefix`, strings.TrimSpace(machine))
	if err != nil {
		return nil, fmt.Errorf("listing worktree mappings: %w", err)
	}
	defer rows.Close()

	mappings := []WorktreeProjectMapping{}
	for rows.Next() {
		m, err := scanWorktreeMapping(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning worktree mapping: %w", err)
		}
		mappings = append(mappings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating worktree mappings: %w", err)
	}
	return mappings, nil
}

func (db *DB) CreateWorktreeProjectMapping(
	ctx context.Context,
	m WorktreeProjectMapping,
) (WorktreeProjectMapping, error) {
	normalized, err := normalizeWorktreeMapping(m.Machine, m.PathPrefix, m.Project)
	if err != nil {
		return WorktreeProjectMapping{}, err
	}

	enabled := 0
	if m.Enabled {
		enabled = 1
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	res, err := db.getWriter().ExecContext(ctx, `
		INSERT INTO worktree_project_mappings (machine, path_prefix, project, enabled)
		VALUES (?, ?, ?, ?)`,
		normalized.Machine,
		normalized.PathPrefix,
		normalized.Project,
		enabled,
	)
	if err != nil {
		if isSQLiteUniqueConstraint(err) {
			return WorktreeProjectMapping{}, ErrWorktreeMappingDuplicate
		}
		return WorktreeProjectMapping{}, fmt.Errorf("creating worktree mapping: %w", err)
	}
	normalized.ID, _ = res.LastInsertId()
	return db.getWorktreeProjectMappingLocked(ctx, normalized.Machine, normalized.ID)
}

func (db *DB) UpdateWorktreeProjectMapping(
	ctx context.Context,
	machine string,
	id int64,
	patch WorktreeProjectMapping,
) (WorktreeProjectMapping, error) {
	normalized, err := normalizeWorktreeMapping(machine, patch.PathPrefix, patch.Project)
	if err != nil {
		return WorktreeProjectMapping{}, err
	}

	enabled := 0
	if patch.Enabled {
		enabled = 1
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	res, err := db.getWriter().ExecContext(ctx, `
		UPDATE worktree_project_mappings
		SET path_prefix = ?,
			project = ?,
			enabled = ?,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND machine = ?`,
		normalized.PathPrefix,
		normalized.Project,
		enabled,
		id,
		normalized.Machine,
	)
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
	return db.getWorktreeProjectMappingLocked(ctx, normalized.Machine, id)
}

func (db *DB) DeleteWorktreeProjectMapping(
	ctx context.Context,
	machine string,
	id int64,
) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	res, err := db.getWriter().ExecContext(ctx,
		`DELETE FROM worktree_project_mappings WHERE id = ? AND machine = ?`,
		id,
		strings.TrimSpace(machine),
	)
	if err != nil {
		return fmt.Errorf("deleting worktree mapping: %w", err)
	}
	changed, _ := res.RowsAffected()
	if changed == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (db *DB) getWorktreeProjectMappingLocked(
	ctx context.Context,
	machine string,
	id int64,
) (WorktreeProjectMapping, error) {
	row := db.getWriter().QueryRowContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE id = ? AND machine = ?`,
		id,
		machine,
	)
	m, err := scanWorktreeMappingRow(row)
	if err != nil {
		return WorktreeProjectMapping{}, err
	}
	return m, nil
}

func (db *DB) ResolveWorktreeProjectMapping(
	ctx context.Context,
	machine string,
	cwd string,
	currentProject string,
) (string, bool, error) {
	mappings, err := db.activeWorktreeProjectMappings(ctx, machine)
	if err != nil {
		return currentProject, false, err
	}
	project, ok := ResolveWorktreeProjectFromMappings(
		mappings, cwd, currentProject,
	)
	return project, ok, nil
}

// ListActiveWorktreeProjectMappings returns enabled mappings
// for a machine in resolution order, with the longest path
// prefixes first.
func (db *DB) ListActiveWorktreeProjectMappings(
	ctx context.Context,
	machine string,
) ([]WorktreeProjectMapping, error) {
	return db.activeWorktreeProjectMappings(ctx, machine)
}

// CopyWorktreeProjectMappingsFrom copies persistent worktree mappings from a
// source DB into this DB. Omit id so source primary keys cannot shadow
// destination rows; UNIQUE(machine, path_prefix) conflicts preserve existing
// destination mappings.
func (db *DB) CopyWorktreeProjectMappingsFrom(sourcePath string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	ctx := context.Background()
	conn, err := db.getWriter().Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(
		ctx, "ATTACH DATABASE ? AS old_db", sourcePath,
	); err != nil {
		return fmt.Errorf("attaching source db: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "DETACH DATABASE old_db")
	}()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin worktree mapping copy tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if oldDBHasTable(ctx, tx, "worktree_project_mappings") {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO main.worktree_project_mappings
				(machine, path_prefix, project, enabled, created_at, updated_at)
			SELECT machine, path_prefix, project, enabled, created_at, updated_at
			FROM old_db.worktree_project_mappings`); err != nil {
			return fmt.Errorf("copying worktree project mappings: %w", err)
		}
	}

	return tx.Commit()
}

// ResolveWorktreeProjectFromMappings applies the same longest
// prefix worktree mapping semantics as ResolveWorktreeProjectMapping
// to an already-loaded mapping set. It defensively sorts a copy so
// callers cannot accidentally depend on input order.
func ResolveWorktreeProjectFromMappings(
	mappings []WorktreeProjectMapping,
	cwd string,
	currentProject string,
) (string, bool) {
	mappings = sortedWorktreeProjectMappings(mappings)
	return ResolveWorktreeProjectFromSortedMappings(
		mappings, cwd, currentProject,
	)
}

// ResolveWorktreeProjectFromSortedMappings applies longest-prefix
// semantics to a mapping set already sorted by descending path prefix
// length. Use this in hot paths with mappings loaded by
// ListActiveWorktreeProjectMappings.
func ResolveWorktreeProjectFromSortedMappings(
	mappings []WorktreeProjectMapping,
	cwd string,
	currentProject string,
) (string, bool) {
	if mapping, ok := bestWorktreeProjectMapping(mappings, cwd); ok {
		return mapping.Project, true
	}
	return currentProject, false
}

func sortedWorktreeProjectMappings(
	mappings []WorktreeProjectMapping,
) []WorktreeProjectMapping {
	sorted := append([]WorktreeProjectMapping(nil), mappings...)
	sortWorktreeProjectMappings(sorted)
	return sorted
}

func sortWorktreeProjectMappings(mappings []WorktreeProjectMapping) {
	sort.SliceStable(mappings, func(i, j int) bool {
		left := mappings[i].PathPrefix
		right := mappings[j].PathPrefix
		if len(left) != len(right) {
			return len(left) > len(right)
		}
		return left < right
	})
}

func (db *DB) activeWorktreeProjectMappings(
	ctx context.Context,
	machine string,
) ([]WorktreeProjectMapping, error) {
	rows, err := db.getReader().QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ? AND enabled = 1
		ORDER BY length(path_prefix) DESC, path_prefix`,
		strings.TrimSpace(machine),
	)
	if err != nil {
		return nil, fmt.Errorf("querying active worktree mappings: %w", err)
	}
	defer rows.Close()

	mappings := []WorktreeProjectMapping{}
	for rows.Next() {
		m, err := scanWorktreeMapping(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning active worktree mapping: %w", err)
		}
		mappings = append(mappings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating active worktree mappings: %w", err)
	}
	return mappings, nil
}

func bestWorktreeProjectMapping(
	mappings []WorktreeProjectMapping,
	cwd string,
) (WorktreeProjectMapping, bool) {
	for _, mapping := range mappings {
		if worktreePathMatches(mapping.PathPrefix, cwd) {
			return mapping, true
		}
	}
	return WorktreeProjectMapping{}, false
}

func (db *DB) ApplyWorktreeProjectMappings(
	ctx context.Context,
	machine string,
) (ApplyWorktreeProjectMappingsResult, error) {
	return db.applyWorktreeProjectMappings(ctx, machine, true)
}

func (db *DB) ApplyWorktreeProjectMappingsFromSync(
	ctx context.Context,
	machine string,
) (ApplyWorktreeProjectMappingsResult, error) {
	return db.applyWorktreeProjectMappings(ctx, machine, false)
}

func (db *DB) applyWorktreeProjectMappings(
	ctx context.Context,
	machine string,
	bumpLocalModifiedAt bool,
) (ApplyWorktreeProjectMappingsResult, error) {
	machine = strings.TrimSpace(machine)

	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.getWriter().BeginTx(ctx, nil)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"beginning worktree mapping apply: %w", err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	mappingRows, err := tx.QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ? AND enabled = 1
		ORDER BY length(path_prefix) DESC, path_prefix`,
		machine,
	)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"querying active worktree mappings: %w", err,
		)
	}
	mappings := []WorktreeProjectMapping{}
	for mappingRows.Next() {
		m, err := scanWorktreeMapping(mappingRows)
		if err != nil {
			mappingRows.Close()
			return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
				"scanning active worktree mapping: %w", err,
			)
		}
		mappings = append(mappings, m)
	}
	if err := mappingRows.Err(); err != nil {
		mappingRows.Close()
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"iterating active worktree mappings: %w", err,
		)
	}
	if err := mappingRows.Close(); err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"closing active worktree mapping rows: %w", err,
		)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, project, cwd
		FROM sessions
		WHERE machine = ? AND cwd != '' AND deleted_at IS NULL`,
		machine,
	)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"querying sessions for worktree mapping apply: %w", err,
		)
	}

	type sessionUpdate struct {
		id      string
		project string
	}
	var updates []sessionUpdate
	var result ApplyWorktreeProjectMappingsResult
	for rows.Next() {
		var id string
		var project string
		var cwd string
		if err := rows.Scan(&id, &project, &cwd); err != nil {
			rows.Close()
			return result, fmt.Errorf("scanning session for worktree mapping apply: %w", err)
		}
		mapping, ok := bestWorktreeProjectMapping(mappings, cwd)
		if !ok {
			continue
		}
		result.MatchedSessions++
		if project != mapping.Project {
			updates = append(updates, sessionUpdate{
				id:      id,
				project: mapping.Project,
			})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, fmt.Errorf("iterating sessions for worktree mapping apply: %w", err)
	}
	if err := rows.Close(); err != nil {
		return result, fmt.Errorf("closing worktree mapping apply rows: %w", err)
	}

	for _, update := range updates {
		updateSQL := `
			UPDATE sessions
			SET project = ?
			WHERE id = ? AND machine = ? AND deleted_at IS NULL AND project != ?`
		if bumpLocalModifiedAt {
			updateSQL = `
				UPDATE sessions
				SET project = ?,
					local_modified_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
				WHERE id = ? AND machine = ? AND deleted_at IS NULL AND project != ?`
		}
		res, err := tx.ExecContext(ctx, updateSQL,
			update.project,
			update.id,
			machine,
			update.project,
		)
		if err != nil {
			return result, fmt.Errorf(
				"applying worktree mapping to session %s: %w",
				update.id,
				err,
			)
		}
		changed, _ := res.RowsAffected()
		result.UpdatedSessions += int(changed)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf("committing worktree mapping apply: %w", err)
	}
	return result, nil
}

func (db *DB) ApplyWorktreeProjectMappingToSession(
	ctx context.Context,
	machine string,
	sessionID string,
	cwd string,
	currentProject string,
) (bool, error) {
	return db.applyWorktreeProjectMappingToSession(
		ctx, machine, sessionID, cwd, currentProject, true,
	)
}

func (db *DB) ApplyWorktreeProjectMappingToSessionFromSync(
	ctx context.Context,
	machine string,
	sessionID string,
	cwd string,
	currentProject string,
) (bool, error) {
	return db.applyWorktreeProjectMappingToSession(
		ctx, machine, sessionID, cwd, currentProject, false,
	)
}

func (db *DB) applyWorktreeProjectMappingToSession(
	ctx context.Context,
	machine string,
	sessionID string,
	cwd string,
	currentProject string,
	bumpLocalModifiedAt bool,
) (bool, error) {
	machine = strings.TrimSpace(machine)
	sessionID = strings.TrimSpace(sessionID)
	if machine == "" || sessionID == "" || strings.TrimSpace(cwd) == "" {
		return false, nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.getWriter().BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf(
			"beginning worktree mapping session apply: %w", err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	mappingRows, err := tx.QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE machine = ? AND enabled = 1
		ORDER BY length(path_prefix) DESC, path_prefix`,
		machine,
	)
	if err != nil {
		return false, fmt.Errorf(
			"querying active worktree mappings: %w", err,
		)
	}
	mappings := []WorktreeProjectMapping{}
	for mappingRows.Next() {
		m, err := scanWorktreeMapping(mappingRows)
		if err != nil {
			mappingRows.Close()
			return false, fmt.Errorf(
				"scanning active worktree mapping: %w", err,
			)
		}
		mappings = append(mappings, m)
	}
	if err := mappingRows.Err(); err != nil {
		mappingRows.Close()
		return false, fmt.Errorf(
			"iterating active worktree mappings: %w", err,
		)
	}
	if err := mappingRows.Close(); err != nil {
		return false, fmt.Errorf(
			"closing active worktree mapping rows: %w", err,
		)
	}

	var rowProject string
	var rowCwd string
	err = tx.QueryRowContext(ctx, `
		SELECT project, cwd
		FROM sessions
		WHERE id = ? AND machine = ? AND deleted_at IS NULL`,
		sessionID,
		machine,
	).Scan(&rowProject, &rowCwd)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf(
			"reading session %s for worktree mapping apply: %w",
			sessionID,
			err,
		)
	}

	mapping, ok := bestWorktreeProjectMapping(mappings, rowCwd)
	if !ok || mapping.Project == rowProject {
		return false, nil
	}

	updateSQL := `
		UPDATE sessions
		SET project = ?
		WHERE id = ?
			AND machine = ?
			AND deleted_at IS NULL
			AND cwd = ?
			AND project = ?`
	if bumpLocalModifiedAt {
		updateSQL = `
			UPDATE sessions
			SET project = ?,
				local_modified_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE id = ?
				AND machine = ?
				AND deleted_at IS NULL
				AND cwd = ?
				AND project = ?`
	}
	res, err := tx.ExecContext(ctx, updateSQL,
		mapping.Project,
		sessionID,
		machine,
		rowCwd,
		rowProject,
	)
	if err != nil {
		return false, fmt.Errorf(
			"applying worktree mapping to session %s: %w",
			sessionID,
			err,
		)
	}
	changed, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf(
			"committing worktree mapping session apply: %w", err,
		)
	}
	return changed > 0, nil
}

func (db *DB) ApplyWorktreeProjectMappingsToSessionsByPath(
	ctx context.Context,
	filePath string,
) (ApplyWorktreeProjectMappingsResult, error) {
	return db.applyWorktreeProjectMappingsToSessionsByPath(
		ctx, filePath, true,
	)
}

func (db *DB) ApplyWorktreeProjectMappingsToSessionsByPathFromSync(
	ctx context.Context,
	filePath string,
) (ApplyWorktreeProjectMappingsResult, error) {
	return db.applyWorktreeProjectMappingsToSessionsByPath(
		ctx, filePath, false,
	)
}

func (db *DB) applyWorktreeProjectMappingsToSessionsByPath(
	ctx context.Context,
	filePath string,
	bumpLocalModifiedAt bool,
) (ApplyWorktreeProjectMappingsResult, error) {
	if filePath == "" {
		return ApplyWorktreeProjectMappingsResult{}, nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	tx, err := db.getWriter().BeginTx(ctx, nil)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"beginning worktree mapping path apply: %w", err,
		)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, machine, project, cwd
		FROM sessions
		WHERE file_path = ? AND cwd != '' AND deleted_at IS NULL`,
		filePath,
	)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"querying sessions for worktree mapping path apply: %w", err,
		)
	}

	type sessionRow struct {
		id      string
		machine string
		project string
		cwd     string
	}
	var sessions []sessionRow
	machines := map[string]bool{}
	for rows.Next() {
		var row sessionRow
		if err := rows.Scan(
			&row.id, &row.machine, &row.project, &row.cwd,
		); err != nil {
			rows.Close()
			return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
				"scanning session for worktree mapping path apply: %w",
				err,
			)
		}
		sessions = append(sessions, row)
		machines[row.machine] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"iterating sessions for worktree mapping path apply: %w",
			err,
		)
	}
	if err := rows.Close(); err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"closing worktree mapping path apply rows: %w", err,
		)
	}
	if len(sessions) == 0 {
		return ApplyWorktreeProjectMappingsResult{}, nil
	}

	mappingRows, err := tx.QueryContext(ctx, `
		SELECT id, machine, path_prefix, project, enabled, created_at, updated_at
		FROM worktree_project_mappings
		WHERE enabled = 1
		ORDER BY machine, length(path_prefix) DESC, path_prefix`,
	)
	if err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"querying active worktree mappings: %w", err,
		)
	}
	mappingsByMachine := map[string][]WorktreeProjectMapping{}
	for mappingRows.Next() {
		m, err := scanWorktreeMapping(mappingRows)
		if err != nil {
			mappingRows.Close()
			return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
				"scanning active worktree mapping: %w", err,
			)
		}
		if machines[m.Machine] {
			mappingsByMachine[m.Machine] = append(
				mappingsByMachine[m.Machine], m,
			)
		}
	}
	if err := mappingRows.Err(); err != nil {
		mappingRows.Close()
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"iterating active worktree mappings: %w", err,
		)
	}
	if err := mappingRows.Close(); err != nil {
		return ApplyWorktreeProjectMappingsResult{}, fmt.Errorf(
			"closing active worktree mapping rows: %w", err,
		)
	}

	var result ApplyWorktreeProjectMappingsResult
	for _, session := range sessions {
		mapping, ok := bestWorktreeProjectMapping(
			mappingsByMachine[session.machine],
			session.cwd,
		)
		if !ok {
			continue
		}
		result.MatchedSessions++
		if mapping.Project == session.project {
			continue
		}
		updateSQL := `
			UPDATE sessions
			SET project = ?
			WHERE id = ?
				AND machine = ?
				AND deleted_at IS NULL
				AND cwd = ?
				AND project = ?`
		if bumpLocalModifiedAt {
			updateSQL = `
				UPDATE sessions
				SET project = ?,
					local_modified_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
				WHERE id = ?
					AND machine = ?
					AND deleted_at IS NULL
					AND cwd = ?
					AND project = ?`
		}
		res, err := tx.ExecContext(ctx, updateSQL,
			mapping.Project,
			session.id,
			session.machine,
			session.cwd,
			session.project,
		)
		if err != nil {
			return result, fmt.Errorf(
				"applying worktree mapping to session %s: %w",
				session.id,
				err,
			)
		}
		changed, _ := res.RowsAffected()
		result.UpdatedSessions += int(changed)
	}
	if err := tx.Commit(); err != nil {
		return result, fmt.Errorf(
			"committing worktree mapping path apply: %w", err,
		)
	}
	return result, nil
}

func isSQLiteUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) &&
		sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
}
