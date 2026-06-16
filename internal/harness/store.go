package harness

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store interface {
	ListProjects() ([]Project, error)
	SaveProjects([]Project) error
	ListMCPServers() ([]MCPServerConfig, error)
	SaveMCPServers([]MCPServerConfig) error
	ListApprovals() ([]ApprovalRecord, error)
	SaveApproval(ApprovalRecord) error
	LoadApproval(string) (ApprovalRecord, error)
	LoadSessionState(string) SessionState
	SaveSessionState(SessionState) error
	RecordTurn(string, RunRequest, RunResponse) error
	ListSessions(projectID string, limit int) ([]SessionRecord, error)
	GetSession(string) (SessionRecord, []TurnRecord, error)
	ListToolCalls(sessionID string) ([]ToolCallRecord, error)
	SaveWorkspaceVersion(WorkspaceVersion) error
	LoadWorkspaceVersion(string) (WorkspaceVersion, error)
	AppendHistoryEvent(HistoryEvent) error
	ListHistoryEvents(projectID, sessionID string, limit int, includeDiff bool) ([]HistoryEvent, error)
	GetHistoryEvent(string) (HistoryEvent, error)
}

var (
	defaultStoreMu sync.Mutex
)

func DefaultStore() (Store, error) {
	path, err := DBPath()
	if err != nil {
		return nil, err
	}
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()
	return OpenSQLiteStore(path)
}

type SQLiteStore struct {
	db         *sql.DB
	closeAfter bool
}

func OpenSQLiteStore(path string) (*SQLiteStore, error) {
	_, statErr := os.Stat(path)
	fresh := errors.Is(statErr, os.ErrNotExist)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if fresh {
		if err := store.importLegacy(); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	store.closeAfter = true
	return store, nil
}

func (s *SQLiteStore) closeIfNeeded() {
	if s.closeAfter && s.db != nil {
		_ = s.db.Close()
	}
}

func (s *SQLiteStore) migrate() error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', default_mode TEXT NOT NULL, allowed_toolsets_json TEXT NOT NULL DEFAULT '[]')`,
		`CREATE TABLE IF NOT EXISTS mcp_servers (id TEXT PRIMARY KEY, name TEXT NOT NULL, transport TEXT NOT NULL, command TEXT NOT NULL DEFAULT '', args_json TEXT NOT NULL DEFAULT '[]', endpoint TEXT NOT NULL DEFAULT '', env_json TEXT NOT NULL DEFAULT '{}', trusted INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS approvals (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, project TEXT NOT NULL DEFAULT '', tool TEXT NOT NULL, args_json TEXT NOT NULL, reason TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sessions (id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, project_id TEXT NOT NULL DEFAULT '', project_name TEXT NOT NULL DEFAULT '', workspace_root TEXT NOT NULL DEFAULT '', mode TEXT NOT NULL DEFAULT '', access_mode TEXT NOT NULL DEFAULT '', active_skills_json TEXT NOT NULL DEFAULT '[]')`,
		`CREATE TABLE IF NOT EXISTS turns (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, timestamp TEXT NOT NULL, status TEXT NOT NULL, request_json TEXT NOT NULL, response_json TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS tool_calls (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, turn_id TEXT NOT NULL, call_index INTEGER NOT NULL, tool TEXT NOT NULL, status TEXT NOT NULL, args_json TEXT NOT NULL DEFAULT '{}', result_json TEXT NOT NULL DEFAULT 'null', error TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS history_events (id TEXT PRIMARY KEY, timestamp TEXT NOT NULL, session_id TEXT NOT NULL, project_id TEXT NOT NULL DEFAULT '', project_name TEXT NOT NULL DEFAULT '', workspace_root TEXT NOT NULL, mode TEXT NOT NULL, step INTEGER NOT NULL, tool TEXT NOT NULL, status TEXT NOT NULL, args_json TEXT NOT NULL DEFAULT '{}', error TEXT NOT NULL DEFAULT '', before_version TEXT NOT NULL, after_version TEXT NOT NULL, diff TEXT NOT NULL DEFAULT '', diff_truncated INTEGER NOT NULL DEFAULT 0, snapshot_notice TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS history_versions (id TEXT PRIMARY KEY, timestamp TEXT NOT NULL, session_id TEXT NOT NULL, project_id TEXT NOT NULL DEFAULT '', project_name TEXT NOT NULL DEFAULT '', workspace_root TEXT NOT NULL, mode TEXT NOT NULL, step INTEGER NOT NULL, tool TEXT NOT NULL, label TEXT NOT NULL, snapshot_json TEXT NOT NULL DEFAULT '', snapshot_path TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS session_skills (session_id TEXT NOT NULL, skill_name TEXT NOT NULL, PRIMARY KEY(session_id, skill_name))`,
		`CREATE INDEX IF NOT EXISTS idx_turns_session ON turns(session_id, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id, call_index)`,
		`CREATE INDEX IF NOT EXISTS idx_history_project ON history_events(project_id, timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_history_session ON history_events(session_id, timestamp DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("history_versions", "snapshot_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES('v1', ?)`, nowUTC())
	return err
}

func (s *SQLiteStore) ensureColumn(table, column, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + definition)
	return err
}

func (s *SQLiteStore) importLegacy() error {
	projects, err := loadProjectsLegacy()
	if err != nil {
		return err
	}
	if len(projects) > 0 {
		if err := s.SaveProjects(projects); err != nil {
			return err
		}
	}
	servers, err := loadMCPServersLegacy()
	if err != nil {
		return err
	}
	if len(servers) > 0 {
		if err := s.SaveMCPServers(servers); err != nil {
			return err
		}
	}
	approvals, err := loadApprovalsLegacy()
	if err != nil {
		return err
	}
	for _, approval := range approvals {
		if err := s.SaveApproval(approval); err != nil {
			return err
		}
	}
	versions, err := loadWorkspaceVersionsLegacy()
	if err != nil {
		return err
	}
	for _, version := range versions {
		if err := s.SaveWorkspaceVersion(version); err != nil {
			return err
		}
	}
	events, err := loadHistoryEventsLegacy()
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := s.AppendHistoryEvent(event); err != nil {
			return err
		}
	}
	states, err := loadSessionStatesLegacy()
	if err != nil {
		return err
	}
	for _, state := range states {
		if err := s.SaveSessionState(state); err != nil {
			return err
		}
	}
	turns, err := loadSessionTurnsLegacy()
	if err != nil {
		return err
	}
	for _, turn := range turns {
		if err := s.RecordTurn(turn.SessionID, turn.Request, turn.Response); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ListProjects() ([]Project, error) {
	defer s.closeIfNeeded()
	rows, err := s.db.Query(`SELECT id, name, path, description, default_mode, allowed_toolsets_json FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		var project Project
		var allowed string
		if err := rows.Scan(&project.ID, &project.Name, &project.Path, &project.Description, &project.DefaultMode, &allowed); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(allowed), &project.AllowedToolsets)
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *SQLiteStore) SaveProjects(projects []Project) error {
	defer s.closeIfNeeded()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM projects`); err != nil {
		return err
	}
	for _, project := range projects {
		allowed := jsonString(project.AllowedToolsets)
		if _, err := tx.Exec(`INSERT INTO projects(id, name, path, description, default_mode, allowed_toolsets_json) VALUES(?, ?, ?, ?, ?, ?)`,
			project.ID, project.Name, project.Path, project.Description, project.DefaultMode, allowed); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListMCPServers() ([]MCPServerConfig, error) {
	defer s.closeIfNeeded()
	rows, err := s.db.Query(`SELECT id, name, transport, command, args_json, endpoint, env_json, trusted FROM mcp_servers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []MCPServerConfig
	for rows.Next() {
		var server MCPServerConfig
		var argsJSON, envJSON string
		var trusted int
		if err := rows.Scan(&server.ID, &server.Name, &server.Transport, &server.Command, &argsJSON, &server.Endpoint, &envJSON, &trusted); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(argsJSON), &server.Args)
		_ = json.Unmarshal([]byte(envJSON), &server.Env)
		server.Trusted = trusted == 1
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (s *SQLiteStore) SaveMCPServers(servers []MCPServerConfig) error {
	defer s.closeIfNeeded()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM mcp_servers`); err != nil {
		return err
	}
	for _, server := range servers {
		trusted := 0
		if server.Trusted {
			trusted = 1
		}
		if _, err := tx.Exec(`INSERT INTO mcp_servers(id, name, transport, command, args_json, endpoint, env_json, trusted) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			server.ID, server.Name, server.Transport, server.Command, jsonString(server.Args), server.Endpoint, jsonString(server.Env), trusted); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListApprovals() ([]ApprovalRecord, error) {
	defer s.closeIfNeeded()
	rows, err := s.db.Query(`SELECT id, session_id, project, tool, args_json, reason, status, created_at, updated_at FROM approvals ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ApprovalRecord
	for rows.Next() {
		record, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *SQLiteStore) SaveApproval(record ApprovalRecord) error {
	defer s.closeIfNeeded()
	_, err := s.db.Exec(`INSERT INTO approvals(id, session_id, project, tool, args_json, reason, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id, project=excluded.project, tool=excluded.tool, args_json=excluded.args_json, reason=excluded.reason, status=excluded.status, created_at=excluded.created_at, updated_at=excluded.updated_at`,
		record.ID, record.SessionID, record.Project, record.Tool, jsonString(record.Args), record.Reason, record.Status, record.CreatedAt, record.UpdatedAt)
	return err
}

func (s *SQLiteStore) LoadApproval(id string) (ApprovalRecord, error) {
	defer s.closeIfNeeded()
	row := s.db.QueryRow(`SELECT id, session_id, project, tool, args_json, reason, status, created_at, updated_at FROM approvals WHERE id=?`, id)
	record, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ApprovalRecord{}, fmt.Errorf("approval not found: %s", id)
	}
	return record, err
}

func (s *SQLiteStore) LoadSessionState(sessionID string) SessionState {
	defer s.closeIfNeeded()
	state := SessionState{ID: sessionID}
	rows, err := s.db.Query(`SELECT skill_name FROM session_skills WHERE session_id=? ORDER BY skill_name`, sessionID)
	if err != nil {
		return state
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			state.ActiveSkills = append(state.ActiveSkills, name)
		}
	}
	return state
}

func (s *SQLiteStore) SaveSessionState(state SessionState) error {
	defer s.closeIfNeeded()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM session_skills WHERE session_id=?`, state.ID); err != nil {
		return err
	}
	for _, name := range state.ActiveSkills {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO session_skills(session_id, skill_name) VALUES(?, ?)`, state.ID, name); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE sessions SET active_skills_json=?, updated_at=? WHERE id=?`, jsonString(state.ActiveSkills), nowUTC(), state.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) RecordTurn(sessionID string, req RunRequest, res RunResponse) error {
	defer s.closeIfNeeded()
	now := nowUTC()
	projectID, projectName := "", ""
	if res.Project != nil {
		projectID = res.Project.ID
		projectName = res.Project.Name
	}
	active := jsonString(res.ActiveSkills)
	_, err := s.db.Exec(`INSERT INTO sessions(id, created_at, updated_at, project_id, project_name, workspace_root, mode, access_mode, active_skills_json)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, project_id=excluded.project_id, project_name=excluded.project_name, workspace_root=excluded.workspace_root, mode=excluded.mode, access_mode=excluded.access_mode, active_skills_json=excluded.active_skills_json`,
		sessionID, now, now, projectID, projectName, res.WorkspaceRoot, res.Mode, res.AccessMode, active)
	if err != nil {
		return err
	}
	turnID := fmt.Sprintf("%s-turn-%d", sessionID, time.Now().UnixNano())
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO turns(id, session_id, timestamp, status, request_json, response_json) VALUES(?, ?, ?, ?, ?, ?)`,
		turnID, sessionID, now, res.Status, jsonString(req), jsonString(res)); err != nil {
		return err
	}
	for i, obs := range res.Observations {
		callID := obs.CallID
		if callID == "" {
			callID = fmt.Sprintf("%s-call-%d", turnID, i)
		}
		args := map[string]any{}
		if i < len(res.HistoryEvents) {
			args = res.HistoryEvents[i].Args
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO tool_calls(id, session_id, turn_id, call_index, tool, status, args_json, result_json, error) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			callID, sessionID, turnID, i, obs.Tool, obs.Status, jsonString(args), jsonString(obs.Result), obs.Error); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListSessions(projectID string, limit int) ([]SessionRecord, error) {
	defer s.closeIfNeeded()
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT s.id, s.created_at, s.updated_at, s.project_id, s.project_name, s.workspace_root, s.mode, s.access_mode, s.active_skills_json, COUNT(t.id)
FROM sessions s LEFT JOIN turns t ON t.session_id=s.id`
	var rows *sql.Rows
	var err error
	if projectID != "" {
		rows, err = s.db.Query(query+` WHERE s.project_id=? GROUP BY s.id ORDER BY s.updated_at DESC LIMIT ?`, projectID, limit)
	} else {
		rows, err = s.db.Query(query+` GROUP BY s.id ORDER BY s.updated_at DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []SessionRecord
	for rows.Next() {
		record, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, record)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) GetSession(id string) (SessionRecord, []TurnRecord, error) {
	defer s.closeIfNeeded()
	row := s.db.QueryRow(`SELECT s.id, s.created_at, s.updated_at, s.project_id, s.project_name, s.workspace_root, s.mode, s.access_mode, s.active_skills_json, COUNT(t.id)
FROM sessions s LEFT JOIN turns t ON t.session_id=s.id WHERE s.id=? GROUP BY s.id`, id)
	session, err := scanSession(row)
	if err != nil {
		return SessionRecord{}, nil, err
	}
	rows, err := s.db.Query(`SELECT id, session_id, timestamp, status, request_json, response_json FROM turns WHERE session_id=? ORDER BY timestamp DESC`, id)
	if err != nil {
		return SessionRecord{}, nil, err
	}
	defer rows.Close()
	var turns []TurnRecord
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return SessionRecord{}, nil, err
		}
		turns = append(turns, turn)
	}
	return session, turns, rows.Err()
}

func (s *SQLiteStore) ListToolCalls(sessionID string) ([]ToolCallRecord, error) {
	defer s.closeIfNeeded()
	rows, err := s.db.Query(`SELECT id, session_id, turn_id, call_index, tool, status, args_json, result_json, error FROM tool_calls WHERE session_id=? ORDER BY turn_id DESC, call_index`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var calls []ToolCallRecord
	for rows.Next() {
		call, err := scanToolCall(rows)
		if err != nil {
			return nil, err
		}
		calls = append(calls, call)
	}
	return calls, rows.Err()
}

func (s *SQLiteStore) SaveWorkspaceVersion(version WorkspaceVersion) error {
	defer s.closeIfNeeded()
	snapshotPath, err := saveSnapshotBlob(version)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO history_versions(id, timestamp, session_id, project_id, project_name, workspace_root, mode, step, tool, label, snapshot_json, snapshot_path) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.Timestamp, version.SessionID, version.ProjectID, version.ProjectName, version.WorkspaceRoot, version.Mode, version.Step, version.Tool, version.Label, "", snapshotPath)
	return err
}

func (s *SQLiteStore) LoadWorkspaceVersion(id string) (WorkspaceVersion, error) {
	defer s.closeIfNeeded()
	row := s.db.QueryRow(`SELECT id, timestamp, session_id, project_id, project_name, workspace_root, mode, step, tool, label, snapshot_json, snapshot_path FROM history_versions WHERE id=?`, id)
	version, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceVersion{}, fmt.Errorf("history version not found: %s", id)
	}
	return version, err
}

func (s *SQLiteStore) AppendHistoryEvent(event HistoryEvent) error {
	defer s.closeIfNeeded()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO history_events(id, timestamp, session_id, project_id, project_name, workspace_root, mode, step, tool, status, args_json, error, before_version, after_version, diff, diff_truncated, snapshot_notice) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Timestamp, event.SessionID, event.ProjectID, event.ProjectName, event.WorkspaceRoot, event.Mode, event.Step, event.Tool, event.Status, jsonString(event.Args), event.Error, event.BeforeVersion, event.AfterVersion, event.Diff, boolInt(event.DiffTruncated), event.SnapshotNotice)
	return err
}

func (s *SQLiteStore) ListHistoryEvents(projectID, sessionID string, limit int, includeDiff bool) ([]HistoryEvent, error) {
	defer s.closeIfNeeded()
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, timestamp, session_id, project_id, project_name, workspace_root, mode, step, tool, status, args_json, error, before_version, after_version, diff, diff_truncated, snapshot_notice FROM history_events`
	var args []any
	var filters []string
	if projectID != "" {
		filters = append(filters, "project_id=?")
		args = append(args, projectID)
	}
	if sessionID != "" {
		filters = append(filters, "session_id=?")
		args = append(args, sessionID)
	}
	if len(filters) > 0 {
		query += " WHERE " + strings.Join(filters, " AND ")
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []HistoryEvent
	for rows.Next() {
		event, err := scanHistoryEvent(rows)
		if err != nil {
			return nil, err
		}
		if !includeDiff {
			event.Diff = ""
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) GetHistoryEvent(id string) (HistoryEvent, error) {
	defer s.closeIfNeeded()
	row := s.db.QueryRow(`SELECT id, timestamp, session_id, project_id, project_name, workspace_root, mode, step, tool, status, args_json, error, before_version, after_version, diff, diff_truncated, snapshot_notice FROM history_events WHERE id=?`, id)
	event, err := scanHistoryEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return HistoryEvent{}, errors.New("history event not found: " + id)
	}
	return event, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanApproval(s scanner) (ApprovalRecord, error) {
	var record ApprovalRecord
	var argsJSON string
	if err := s.Scan(&record.ID, &record.SessionID, &record.Project, &record.Tool, &argsJSON, &record.Reason, &record.Status, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return record, err
	}
	_ = json.Unmarshal([]byte(argsJSON), &record.Args)
	return record, nil
}

func scanSession(s scanner) (SessionRecord, error) {
	var record SessionRecord
	var active string
	if err := s.Scan(&record.ID, &record.CreatedAt, &record.UpdatedAt, &record.ProjectID, &record.ProjectName, &record.WorkspaceRoot, &record.Mode, &record.AccessMode, &active, &record.TurnCount); err != nil {
		return record, err
	}
	_ = json.Unmarshal([]byte(active), &record.ActiveSkills)
	return record, nil
}

func scanTurn(s scanner) (TurnRecord, error) {
	var record TurnRecord
	var requestJSON, responseJSON string
	if err := s.Scan(&record.ID, &record.SessionID, &record.Timestamp, &record.Status, &requestJSON, &responseJSON); err != nil {
		return record, err
	}
	_ = json.Unmarshal([]byte(requestJSON), &record.Request)
	_ = json.Unmarshal([]byte(responseJSON), &record.Response)
	return record, nil
}

func scanToolCall(s scanner) (ToolCallRecord, error) {
	var record ToolCallRecord
	var argsJSON, resultJSON string
	if err := s.Scan(&record.ID, &record.SessionID, &record.TurnID, &record.Index, &record.Tool, &record.Status, &argsJSON, &resultJSON, &record.Error); err != nil {
		return record, err
	}
	_ = json.Unmarshal([]byte(argsJSON), &record.Args)
	_ = json.Unmarshal([]byte(resultJSON), &record.Result)
	return record, nil
}

func scanVersion(s scanner) (WorkspaceVersion, error) {
	var version WorkspaceVersion
	var snapshotJSON, snapshotPath string
	if err := s.Scan(&version.ID, &version.Timestamp, &version.SessionID, &version.ProjectID, &version.ProjectName, &version.WorkspaceRoot, &version.Mode, &version.Step, &version.Tool, &version.Label, &snapshotJSON, &snapshotPath); err != nil {
		return version, err
	}
	if snapshotJSON != "" {
		_ = json.Unmarshal([]byte(snapshotJSON), &version.Snapshot)
	} else if snapshotPath != "" {
		snapshot, err := loadSnapshotBlob(snapshotPath)
		if err != nil {
			return version, err
		}
		version.Snapshot = snapshot
	}
	return version, nil
}

func scanHistoryEvent(s scanner) (HistoryEvent, error) {
	var event HistoryEvent
	var argsJSON string
	var diffTruncated int
	if err := s.Scan(&event.ID, &event.Timestamp, &event.SessionID, &event.ProjectID, &event.ProjectName, &event.WorkspaceRoot, &event.Mode, &event.Step, &event.Tool, &event.Status, &argsJSON, &event.Error, &event.BeforeVersion, &event.AfterVersion, &event.Diff, &diffTruncated, &event.SnapshotNotice); err != nil {
		return event, err
	}
	_ = json.Unmarshal([]byte(argsJSON), &event.Args)
	event.DiffTruncated = diffTruncated == 1
	return event, nil
}

func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(data)
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func saveSnapshotBlob(version WorkspaceVersion) (string, error) {
	dir, err := HistoryBlobsDir()
	if err != nil {
		return "", err
	}
	name := filepath.Base(version.ID) + ".json.gz"
	path := filepath.Join(dir, name)
	data, err := json.Marshal(version.Snapshot)
	if err != nil {
		return "", err
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(data); err != nil {
		_ = zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("history", "blobs", name)), nil
}

func loadSnapshotBlob(relPath string) (WorkspaceSnapshot, error) {
	var snapshot WorkspaceSnapshot
	base, err := AppDir()
	if err != nil {
		return snapshot, err
	}
	path := filepath.Clean(filepath.Join(base, filepath.FromSlash(relPath)))
	if !strings.HasPrefix(path, filepath.Clean(base)+string(os.PathSeparator)) && filepath.Clean(path) != filepath.Clean(base) {
		return snapshot, errors.New("snapshot blob path escapes harness home")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return snapshot, err
		}
		defer zr.Close()
		data, err = io.ReadAll(zr)
		if err != nil {
			return snapshot, err
		}
	}
	return snapshot, json.Unmarshal(data, &snapshot)
}

func loadProjectsLegacy() ([]Project, error) {
	path, err := ProjectsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload projectsFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Projects, nil
}

func loadMCPServersLegacy() ([]MCPServerConfig, error) {
	path, err := MCPsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload mcpConfigFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Servers, nil
}

func loadApprovalsLegacy() ([]ApprovalRecord, error) {
	dir, err := ApprovalsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var records []ApprovalRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var record ApprovalRecord
		if json.Unmarshal(data, &record) == nil {
			records = append(records, record)
		}
	}
	return records, nil
}

func loadWorkspaceVersionsLegacy() ([]WorkspaceVersion, error) {
	dir, err := HistoryVersionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var versions []WorkspaceVersion
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var version WorkspaceVersion
		if json.Unmarshal(data, &version) == nil {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

func loadHistoryEventsLegacy() ([]HistoryEvent, error) {
	dir, err := HistoryDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []HistoryEvent
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event HistoryEvent
		if json.Unmarshal([]byte(line), &event) == nil {
			events = append(events, event)
		}
	}
	return events, scanner.Err()
}

func loadSessionStatesLegacy() ([]SessionState, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var states []SessionState
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".state.json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var state SessionState
		if json.Unmarshal(data, &state) == nil {
			states = append(states, state)
		}
	}
	return states, nil
}

func loadSessionTurnsLegacy() ([]TurnRecord, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var turns []TurnRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			var payload struct {
				Timestamp string      `json:"timestamp"`
				Request   RunRequest  `json:"request"`
				Response  RunResponse `json:"response"`
			}
			if json.Unmarshal([]byte(scanner.Text()), &payload) == nil {
				turns = append(turns, TurnRecord{
					ID:        fmt.Sprintf("%s-import-%d", payload.Response.SessionID, len(turns)),
					SessionID: payload.Response.SessionID,
					Timestamp: payload.Timestamp,
					Status:    payload.Response.Status,
					Request:   payload.Request,
					Response:  payload.Response,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return turns, nil
}
