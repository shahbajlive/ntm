package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// testStore creates a test store with in-memory SQLite and runs migrations.
func testStore(t *testing.T) *Store {
	t.Helper()

	// Use in-memory SQLite for tests - no WAL mode needed
	db, err := sql.Open("sqlite3", ":memory:?_foreign_keys=ON")
	if err != nil {
		t.Fatalf("Open in-memory db error: %v", err)
	}

	store := &Store{db: db, path: ":memory:"}

	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate() error: %v", err)
	}

	t.Cleanup(func() { store.Close() })

	return store
}

// ======================
// Schema Types Tests
// ======================

func TestSessionStatusConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status SessionStatus
		want   string
	}{
		{SessionActive, "active"},
		{SessionPaused, "paused"},
		{SessionTerminated, "terminated"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("SessionStatus %v = %q, want %q", tt.status, string(tt.status), tt.want)
		}
	}
}

func TestAgentStatusConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status AgentStatus
		want   string
	}{
		{AgentIdle, "idle"},
		{AgentWorking, "working"},
		{AgentError, "error"},
		{AgentCrashed, "crashed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("AgentStatus %v = %q, want %q", tt.status, string(tt.status), tt.want)
		}
	}
}

func TestAgentTypeConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agentType AgentType
		want      string
	}{
		{AgentTypeClaude, "cc"},
		{AgentTypeCodex, "cod"},
		{AgentTypeGemini, "gmi"},
	}

	for _, tt := range tests {
		if string(tt.agentType) != tt.want {
			t.Errorf("AgentType %v = %q, want %q", tt.agentType, string(tt.agentType), tt.want)
		}
	}
}

func TestTaskStatusConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status TaskStatus
		want   string
	}{
		{TaskPending, "pending"},
		{TaskAssigned, "assigned"},
		{TaskWorking, "working"},
		{TaskCompleted, "completed"},
		{TaskFailed, "failed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("TaskStatus %v = %q, want %q", tt.status, string(tt.status), tt.want)
		}
	}
}

func TestTaskResultConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		result TaskResult
		want   string
	}{
		{TaskResultSuccess, "success"},
		{TaskResultFailure, "failure"},
		{TaskResultPartial, "partial"},
	}

	for _, tt := range tests {
		if string(tt.result) != tt.want {
			t.Errorf("TaskResult %v = %q, want %q", tt.result, string(tt.result), tt.want)
		}
	}
}

func TestApprovalStatusConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status ApprovalStatus
		want   string
	}{
		{ApprovalPending, "pending"},
		{ApprovalApproved, "approved"},
		{ApprovalDenied, "denied"},
		{ApprovalExpired, "expired"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("ApprovalStatus %v = %q, want %q", tt.status, string(tt.status), tt.want)
		}
	}
}

// ======================
// Store Basic Tests
// ======================

func TestOpenDefault(t *testing.T) {
	// Test default path (uses user home dir)
	// Save and restore HOME to avoid affecting real config
	origHome := os.Getenv("HOME")
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	store, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") error: %v", err)
	}
	defer store.Close()

	// Check default path
	wantPath := filepath.Join(tmpDir, ".config", "ntm", "state.db")
	if store.Path() != wantPath {
		t.Errorf("Path() = %q, want %q", store.Path(), wantPath)
	}
}

func TestOpenCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	customPath := filepath.Join(tmpDir, "custom", "db.sqlite")

	store, err := Open(customPath)
	if err != nil {
		t.Fatalf("Open(%q) error: %v", customPath, err)
	}
	defer store.Close()

	if store.Path() != customPath {
		t.Errorf("Path() = %q, want %q", store.Path(), customPath)
	}

	// Verify file was created
	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Errorf("Database file was not created at %q", customPath)
	}
}

func TestMigrate(t *testing.T) {
	store := testStore(t)

	// First, let's see what tables actually exist
	rows, err := store.db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatalf("Query sqlite_master error: %v", err)
	}
	var existingTables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan error: %v", err)
		}
		existingTables = append(existingTables, name)
	}
	rows.Close()
	t.Logf("Existing tables: %v", existingTables)

	// Verify tables exist by trying to query them
	tables := []string{"sessions", "agents", "tasks", "reservations", "approvals", "context_packs", "tool_health", "event_log", "_migrations"}
	for _, table := range tables {
		r, err := store.db.Query("SELECT 1 FROM " + table + " LIMIT 1")
		if err != nil {
			t.Errorf("Table %q should exist after migration: %v", table, err)
		} else {
			r.Close()
		}
	}
}

func TestTransaction(t *testing.T) {
	store := testStore(t)

	// Create a session within a transaction
	sess := &Session{
		ID:          "tx-sess-1",
		Name:        "tx-session",
		ProjectPath: "/test/project",
		CreatedAt:   time.Now(),
		Status:      SessionActive,
	}

	err := store.Transaction(func(tx *Tx) error {
		_, err := tx.tx.Exec(`
			INSERT INTO sessions (id, name, project_path, created_at, status)
			VALUES (?, ?, ?, ?, ?)`,
			sess.ID, sess.Name, sess.ProjectPath, sess.CreatedAt, sess.Status,
		)
		return err
	})
	if err != nil {
		t.Fatalf("Transaction error: %v", err)
	}

	// Verify session was created
	got, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if got == nil || got.ID != sess.ID {
		t.Errorf("Session not found after transaction")
	}
}

func TestTransactionRollback(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID:          "tx-rollback-1",
		Name:        "rollback-session",
		ProjectPath: "/test/rollback",
		CreatedAt:   time.Now(),
		Status:      SessionActive,
	}

	// Transaction that returns an error should rollback
	err := store.Transaction(func(tx *Tx) error {
		_, err := tx.tx.Exec(`
			INSERT INTO sessions (id, name, project_path, created_at, status)
			VALUES (?, ?, ?, ?, ?)`,
			sess.ID, sess.Name, sess.ProjectPath, sess.CreatedAt, sess.Status,
		)
		if err != nil {
			return err
		}
		// Return error to trigger rollback
		return sql.ErrNoRows // Using this as a test error
	})
	if err == nil {
		t.Fatal("Transaction should have returned error")
	}

	// Verify session was NOT created
	got, _ := store.GetSession(sess.ID)
	if got != nil {
		t.Errorf("Session should not exist after rollback")
	}
}

// ======================
// Session Operations Tests
// ======================

func TestSessionCRUD(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID:               "sess-1",
		Name:             "test-session",
		ProjectPath:      "/test/project",
		CreatedAt:        time.Now().UTC().Round(time.Second),
		Status:           SessionActive,
		ConfigSnapshot:   `{"agents": 2}`,
		CoordinatorAgent: "GreenLake",
	}

	// Create
	if err := store.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}

	// Read
	got, err := store.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if got == nil {
		t.Fatal("GetSession returned nil")
	}
	if got.Name != sess.Name || got.Status != sess.Status {
		t.Errorf("GetSession = %+v, want name=%q status=%v", got, sess.Name, sess.Status)
	}

	// Update
	sess.Status = SessionPaused
	sess.ConfigSnapshot = `{"agents": 3}`
	if err := store.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession error: %v", err)
	}

	got, _ = store.GetSession(sess.ID)
	if got.Status != SessionPaused {
		t.Errorf("After update, Status = %v, want %v", got.Status, SessionPaused)
	}

	// Delete
	if err := store.DeleteSession(sess.ID); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}

	got, _ = store.GetSession(sess.ID)
	if got != nil {
		t.Error("Session should be nil after delete")
	}
}

func TestGetSessionNotFound(t *testing.T) {
	store := testStore(t)

	got, err := store.GetSession("nonexistent")
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if got != nil {
		t.Errorf("GetSession(nonexistent) = %v, want nil", got)
	}
}

func TestListSessions(t *testing.T) {
	store := testStore(t)

	// Create test sessions
	sessions := []*Session{
		{ID: "list-1", Name: "session-1", ProjectPath: "/p1", CreatedAt: time.Now(), Status: SessionActive},
		{ID: "list-2", Name: "session-2", ProjectPath: "/p2", CreatedAt: time.Now(), Status: SessionPaused},
		{ID: "list-3", Name: "session-3", ProjectPath: "/p3", CreatedAt: time.Now(), Status: SessionActive},
	}
	for _, s := range sessions {
		if err := store.CreateSession(s); err != nil {
			t.Fatalf("CreateSession error: %v", err)
		}
	}

	// List all
	all, err := store.ListSessions("")
	if err != nil {
		t.Fatalf("ListSessions(\"\") error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListSessions(\"\") returned %d sessions, want 3", len(all))
	}

	// List by status
	active, err := store.ListSessions("active")
	if err != nil {
		t.Fatalf("ListSessions(active) error: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("ListSessions(active) returned %d sessions, want 2", len(active))
	}

	paused, err := store.ListSessions("paused")
	if err != nil {
		t.Fatalf("ListSessions(paused) error: %v", err)
	}
	if len(paused) != 1 {
		t.Errorf("ListSessions(paused) returned %d sessions, want 1", len(paused))
	}
}

func TestUpdateSessionNotFound(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID:          "nonexistent",
		Name:        "ghost",
		ProjectPath: "/ghost",
		Status:      SessionActive,
	}

	err := store.UpdateSession(sess)
	if err == nil {
		t.Error("UpdateSession should error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found': %v", err)
	}
}

func TestDeleteSessionNotFound(t *testing.T) {
	store := testStore(t)

	err := store.DeleteSession("nonexistent")
	if err == nil {
		t.Error("DeleteSession should error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Error should mention 'not found': %v", err)
	}
}

// ======================
// Agent Operations Tests
// ======================

func TestAgentCRUD(t *testing.T) {
	store := testStore(t)

	// Create session first (foreign key)
	sess := &Session{
		ID: "agent-sess", Name: "agent-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	now := time.Now().UTC().Round(time.Second)
	agent := &Agent{
		ID:              "agent-1",
		SessionID:       sess.ID,
		Name:            "BlueTiger",
		Type:            AgentTypeClaude,
		Model:           "claude-4",
		TmuxPaneID:      "%0",
		LastSeen:        &now,
		Status:          AgentIdle,
		CurrentTaskID:   "task-1",
		PerformanceData: `{"tokens": 1000}`,
	}

	// Create
	if err := store.CreateAgent(agent); err != nil {
		t.Fatalf("CreateAgent error: %v", err)
	}

	// Read by ID
	got, err := store.GetAgent(agent.ID)
	if err != nil {
		t.Fatalf("GetAgent error: %v", err)
	}
	if got == nil || got.Name != agent.Name {
		t.Errorf("GetAgent = %+v, want name=%q", got, agent.Name)
	}

	// Read by name
	got, err = store.GetAgentByName(sess.ID, "BlueTiger")
	if err != nil {
		t.Fatalf("GetAgentByName error: %v", err)
	}
	if got == nil || got.ID != agent.ID {
		t.Errorf("GetAgentByName = %+v, want id=%q", got, agent.ID)
	}

	// Update
	agent.Status = AgentWorking
	if err := store.UpdateAgent(agent); err != nil {
		t.Fatalf("UpdateAgent error: %v", err)
	}

	got, _ = store.GetAgent(agent.ID)
	if got.Status != AgentWorking {
		t.Errorf("After update, Status = %v, want %v", got.Status, AgentWorking)
	}
}

func TestListAgents(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "list-agent-sess", Name: "list-agents", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	agents := []*Agent{
		{ID: "la-1", SessionID: sess.ID, Name: "Alpha", Type: AgentTypeClaude, Status: AgentIdle},
		{ID: "la-2", SessionID: sess.ID, Name: "Beta", Type: AgentTypeCodex, Status: AgentWorking},
	}
	for _, a := range agents {
		store.CreateAgent(a)
	}

	list, err := store.ListAgents(sess.ID)
	if err != nil {
		t.Fatalf("ListAgents error: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("ListAgents returned %d agents, want 2", len(list))
	}
}

// ======================
// Task Operations Tests
// ======================

func TestTaskCRUD(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "task-sess", Name: "task-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	// Create agent for FK reference (agent_id can be NULL, but empty string fails FK check)
	agent := &Agent{
		ID: "task-agent", SessionID: sess.ID, Name: "TaskAgent",
		Type: AgentTypeClaude, Status: AgentIdle,
	}
	store.CreateAgent(agent)

	task := &Task{
		ID:            "task-1",
		SessionID:     sess.ID,
		AgentID:       agent.ID,
		BeadID:        "bead-123",
		CorrelationID: "corr-456",
		Status:        TaskPending,
		CreatedAt:     time.Now().UTC().Round(time.Second),
	}

	// Create
	if err := store.CreateTask(task); err != nil {
		t.Fatalf("CreateTask error: %v", err)
	}

	// Read by ID
	got, err := store.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask error: %v", err)
	}
	if got == nil || got.BeadID != task.BeadID {
		t.Errorf("GetTask = %+v, want bead_id=%q", got, task.BeadID)
	}

	// Read by correlation ID
	got, err = store.GetTaskByCorrelation(task.CorrelationID)
	if err != nil {
		t.Fatalf("GetTaskByCorrelation error: %v", err)
	}
	if got == nil || got.ID != task.ID {
		t.Errorf("GetTaskByCorrelation = %+v, want id=%q", got, task.ID)
	}

	// Update
	now := time.Now()
	task.Status = TaskCompleted
	task.CompletedAt = &now
	result := TaskResultSuccess
	task.Result = &result
	if err := store.UpdateTask(task); err != nil {
		t.Fatalf("UpdateTask error: %v", err)
	}

	got, _ = store.GetTask(task.ID)
	if got.Status != TaskCompleted {
		t.Errorf("After update, Status = %v, want %v", got.Status, TaskCompleted)
	}
}

func TestListTasks(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "list-task-sess", Name: "list-tasks", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	// Create agent for FK reference
	agent := &Agent{
		ID: "list-task-agent", SessionID: sess.ID, Name: "ListTaskAgent",
		Type: AgentTypeClaude, Status: AgentIdle,
	}
	store.CreateAgent(agent)

	tasks := []*Task{
		{ID: "lt-1", SessionID: sess.ID, AgentID: agent.ID, Status: TaskPending, CreatedAt: time.Now()},
		{ID: "lt-2", SessionID: sess.ID, AgentID: agent.ID, Status: TaskCompleted, CreatedAt: time.Now()},
		{ID: "lt-3", SessionID: sess.ID, AgentID: agent.ID, Status: TaskPending, CreatedAt: time.Now()},
	}
	for _, task := range tasks {
		store.CreateTask(task)
	}

	// List all
	all, err := store.ListTasks(sess.ID, "")
	if err != nil {
		t.Fatalf("ListTasks(\"\") error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListTasks(\"\") returned %d tasks, want 3", len(all))
	}

	// List by status
	pending, err := store.ListTasks(sess.ID, "pending")
	if err != nil {
		t.Fatalf("ListTasks(pending) error: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("ListTasks(pending) returned %d tasks, want 2", len(pending))
	}
}

// ======================
// Reservation Operations Tests
// ======================

func TestReservationCRUD(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "res-sess", Name: "res-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	agent := &Agent{
		ID: "res-agent", SessionID: sess.ID, Name: "ResAgent",
		Type: AgentTypeClaude, Status: AgentIdle,
	}
	store.CreateAgent(agent)

	res := &Reservation{
		SessionID:   sess.ID,
		AgentID:     agent.ID,
		PathPattern: "internal/**/*.go",
		Exclusive:   true,
		Reason:      "refactoring",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	// Create
	if err := store.CreateReservation(res); err != nil {
		t.Fatalf("CreateReservation error: %v", err)
	}
	if res.ID == 0 {
		t.Error("Reservation ID should be set after create")
	}

	// Read
	got, err := store.GetReservation(res.ID)
	if err != nil {
		t.Fatalf("GetReservation error: %v", err)
	}
	if got == nil || got.PathPattern != res.PathPattern {
		t.Errorf("GetReservation = %+v, want path_pattern=%q", got, res.PathPattern)
	}

	// Update (release)
	now := time.Now()
	res.ReleasedAt = &now
	if err := store.UpdateReservation(res); err != nil {
		t.Fatalf("UpdateReservation error: %v", err)
	}

	got, _ = store.GetReservation(res.ID)
	if got.ReleasedAt == nil {
		t.Error("ReleasedAt should be set after update")
	}
}

func TestListReservations(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "list-res-sess", Name: "list-res", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	agent := &Agent{
		ID: "list-res-agent", SessionID: sess.ID, Name: "ListResAgent",
		Type: AgentTypeClaude, Status: AgentIdle,
	}
	store.CreateAgent(agent)

	// Create reservations: one active, one expired, one released
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	// Active reservation (future expiry, not released)
	activeRes := &Reservation{SessionID: sess.ID, AgentID: agent.ID, PathPattern: "active/*", Exclusive: true, ExpiresAt: future}
	store.CreateReservation(activeRes)

	// Expired reservation (past expiry, not released)
	expiredRes := &Reservation{SessionID: sess.ID, AgentID: agent.ID, PathPattern: "expired/*", Exclusive: true, ExpiresAt: past}
	store.CreateReservation(expiredRes)

	// Released reservation (future expiry, but released)
	releasedRes := &Reservation{SessionID: sess.ID, AgentID: agent.ID, PathPattern: "released/*", Exclusive: true, ExpiresAt: future}
	store.CreateReservation(releasedRes)
	releasedRes.ReleasedAt = &now
	store.UpdateReservation(releasedRes) // Update to set released_at

	// List all
	all, err := store.ListReservations(sess.ID, false)
	if err != nil {
		t.Fatalf("ListReservations(all) error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListReservations(all) returned %d, want 3", len(all))
	}

	// List active only
	active, err := store.ListReservations(sess.ID, true)
	if err != nil {
		t.Fatalf("ListReservations(active) error: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("ListReservations(active) returned %d, want 1", len(active))
	}
}

func TestFindConflicts(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "conflict-sess", Name: "conflict-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	agent := &Agent{
		ID: "conflict-agent", SessionID: sess.ID, Name: "ConflictAgent",
		Type: AgentTypeClaude, Status: AgentIdle,
	}
	store.CreateAgent(agent)

	// Create exclusive reservation
	res := &Reservation{
		SessionID:   sess.ID,
		AgentID:     agent.ID,
		PathPattern: "internal/*",
		Exclusive:   true,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	store.CreateReservation(res)

	// Check for conflicts
	conflicts, err := store.FindConflicts(sess.ID, "internal/cli/main.go")
	if err != nil {
		t.Fatalf("FindConflicts error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Errorf("FindConflicts returned %d conflicts, want 1", len(conflicts))
	}

	// Non-conflicting path
	noConflicts, err := store.FindConflicts(sess.ID, "cmd/main.go")
	if err != nil {
		t.Fatalf("FindConflicts error: %v", err)
	}
	if len(noConflicts) != 0 {
		t.Errorf("FindConflicts for non-matching path returned %d, want 0", len(noConflicts))
	}
}

// ======================
// Approval Operations Tests
// ======================

func TestApprovalCRUD(t *testing.T) {
	store := testStore(t)

	appr := &Approval{
		ID:          "appr-1",
		Action:      "git push --force",
		Resource:    "origin/main",
		Reason:      "rebase cleanup",
		RequestedBy: "BlueTiger",
		RequiresSLB: true,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
		Status:      ApprovalPending,
	}

	// Create
	if err := store.CreateApproval(appr); err != nil {
		t.Fatalf("CreateApproval error: %v", err)
	}

	// Read
	got, err := store.GetApproval(appr.ID)
	if err != nil {
		t.Fatalf("GetApproval error: %v", err)
	}
	if got == nil || got.Action != appr.Action {
		t.Errorf("GetApproval = %+v, want action=%q", got, appr.Action)
	}

	// Update (approve)
	appr.Status = ApprovalApproved
	appr.ApprovedBy = "human"
	now := time.Now()
	appr.ApprovedAt = &now
	if err := store.UpdateApproval(appr); err != nil {
		t.Fatalf("UpdateApproval error: %v", err)
	}

	got, _ = store.GetApproval(appr.ID)
	if got.Status != ApprovalApproved {
		t.Errorf("After update, Status = %v, want %v", got.Status, ApprovalApproved)
	}
}

func TestListPendingApprovals(t *testing.T) {
	store := testStore(t)

	approvals := []*Approval{
		{ID: "pa-1", Action: "action1", Resource: "r1", RequestedBy: "a1", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Status: ApprovalPending},
		{ID: "pa-2", Action: "action2", Resource: "r2", RequestedBy: "a2", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Status: ApprovalApproved},
		{ID: "pa-3", Action: "action3", Resource: "r3", RequestedBy: "a3", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(-time.Hour), Status: ApprovalPending}, // expired
	}
	for _, a := range approvals {
		store.CreateApproval(a)
	}

	pending, err := store.ListPendingApprovals()
	if err != nil {
		t.Fatalf("ListPendingApprovals error: %v", err)
	}
	if len(pending) != 1 {
		t.Errorf("ListPendingApprovals returned %d, want 1", len(pending))
	}
}

// ======================
// Tool Health Operations Tests
// ======================

func TestToolHealthUpsert(t *testing.T) {
	store := testStore(t)

	now := time.Now()
	th := &ToolHealth{
		Tool:         "cass",
		Version:      "1.0.0",
		Capabilities: `["search", "index"]`,
		LastOK:       &now,
	}

	// Insert
	if err := store.UpsertToolHealth(th); err != nil {
		t.Fatalf("UpsertToolHealth (insert) error: %v", err)
	}

	got, err := store.GetToolHealth("cass")
	if err != nil {
		t.Fatalf("GetToolHealth error: %v", err)
	}
	if got == nil || got.Version != "1.0.0" {
		t.Errorf("GetToolHealth = %+v, want version=1.0.0", got)
	}

	// Update
	th.Version = "2.0.0"
	th.LastError = "connection timeout"
	if err := store.UpsertToolHealth(th); err != nil {
		t.Fatalf("UpsertToolHealth (update) error: %v", err)
	}

	got, _ = store.GetToolHealth("cass")
	if got.Version != "2.0.0" || got.LastError != "connection timeout" {
		t.Errorf("After upsert, got %+v, want version=2.0.0", got)
	}
}

func TestListToolHealth(t *testing.T) {
	store := testStore(t)

	tools := []*ToolHealth{
		{Tool: "bv", Version: "1.0.0"},
		{Tool: "cm", Version: "0.5.0"},
		{Tool: "ubs", Version: "2.0.0"},
	}
	for _, th := range tools {
		store.UpsertToolHealth(th)
	}

	list, err := store.ListToolHealth()
	if err != nil {
		t.Fatalf("ListToolHealth error: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("ListToolHealth returned %d, want 3", len(list))
	}
}

// ======================
// Context Pack Operations Tests
// ======================

func TestContextPackCRUD(t *testing.T) {
	store := testStore(t)

	cp := &ContextPack{
		ID:             "cp-1",
		BeadID:         "bead-123",
		AgentType:      AgentTypeClaude,
		RepoRev:        "abc123",
		CorrelationID:  "corr-1",
		CreatedAt:      time.Now(),
		TokenCount:     5000,
		RenderedPrompt: "Please implement...",
	}

	// Create
	if err := store.CreateContextPack(cp); err != nil {
		t.Fatalf("CreateContextPack error: %v", err)
	}

	// Read
	got, err := store.GetContextPack(cp.ID)
	if err != nil {
		t.Fatalf("GetContextPack error: %v", err)
	}
	if got == nil || got.TokenCount != 5000 {
		t.Errorf("GetContextPack = %+v, want token_count=5000", got)
	}
}

// ======================
// Event Log Operations Tests
// ======================

func TestEventLogCRUD(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "event-sess", Name: "event-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	entry := &EventLogEntry{
		SessionID:     sess.ID,
		EventType:     "task_assigned",
		EventData:     `{"task_id": "t1", "agent_id": "a1"}`,
		CorrelationID: "corr-1",
	}

	// Log event
	if err := store.LogEvent(entry); err != nil {
		t.Fatalf("LogEvent error: %v", err)
	}
	if entry.ID == 0 {
		t.Error("Event ID should be set after log")
	}

	// List events
	events, err := store.ListEvents(sess.ID, 10)
	if err != nil {
		t.Fatalf("ListEvents error: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("ListEvents returned %d events, want 1", len(events))
	}
}

func TestReplayEvents(t *testing.T) {
	store := testStore(t)

	sess := &Session{
		ID: "replay-sess", Name: "replay-test", ProjectPath: "/test",
		CreatedAt: time.Now(), Status: SessionActive,
	}
	store.CreateSession(sess)

	// Log multiple events
	for i := 0; i < 5; i++ {
		entry := &EventLogEntry{
			SessionID: sess.ID,
			EventType: "test_event",
			EventData: `{"seq": ` + string(rune('0'+i)) + `}`,
		}
		store.LogEvent(entry)
	}

	// Replay from ID 2 (should get events 3, 4, 5)
	var replayed []EventLogEntry
	err := store.ReplayEvents(sess.ID, 2, func(e EventLogEntry) error {
		replayed = append(replayed, e)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayEvents error: %v", err)
	}
	if len(replayed) != 3 {
		t.Errorf("ReplayEvents from ID 2 returned %d events, want 3", len(replayed))
	}
}

// ======================
// Migration Tests
// ======================

func TestGetMigrationFiles(t *testing.T) {
	t.Parallel()

	files, err := GetMigrationFiles()
	if err != nil {
		t.Fatalf("GetMigrationFiles error: %v", err)
	}
	if len(files) == 0 {
		t.Error("GetMigrationFiles returned no files")
	}
	// Should have at least the initial migration
	if !strings.HasPrefix(files[0], "001_") {
		t.Errorf("First migration should be 001_*, got %q", files[0])
	}
}

func TestReadMigration(t *testing.T) {
	t.Parallel()

	content, err := ReadMigration("001_initial.sql")
	if err != nil {
		t.Fatalf("ReadMigration error: %v", err)
	}
	if !strings.Contains(content, "CREATE TABLE") {
		t.Error("Migration content should contain CREATE TABLE")
	}
}

func TestReadMigrationNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadMigration("999_nonexistent.sql")
	if err == nil {
		t.Error("ReadMigration should error for nonexistent file")
	}
}

func TestApplyMigrationsIdempotent(t *testing.T) {
	store := testStore(t)

	// Migrate is already called in testStore, call again to test idempotency
	if err := store.Migrate(); err != nil {
		t.Fatalf("Second Migrate() error: %v", err)
	}

	// Get expected migration count
	migrationFiles, err := GetMigrationFiles()
	if err != nil {
		t.Fatalf("GetMigrationFiles error: %v", err)
	}

	// Verify _migrations table has entry for each migration file
	var count int
	err = store.db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count)
	if err != nil {
		t.Fatalf("Query migrations count error: %v", err)
	}
	if count != len(migrationFiles) {
		t.Errorf("Migrations count = %d, want %d", count, len(migrationFiles))
	}
}
