package projectagent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	workDir := t.TempDir()
	a, err := New(Config{
		ProjectID:       "test-proj",
		ControlPlaneURL: "http://localhost:8080",
		WorkDir:         workDir,
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	return a
}

func TestHandleHealth(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	agent.handleHealth(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %v", body["status"])
	}
	if body["project_id"] != "test-proj" {
		t.Errorf("expected project test-proj, got %v", body["project_id"])
	}
}

func TestHandleStatus_Idle(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	agent.handleStatus(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	if body["busy"] != false {
		t.Error("agent should not be busy")
	}
	if body["project_id"] != "test-proj" {
		t.Errorf("got project %v", body["project_id"])
	}
}

func TestHandleTask_MethodNotAllowed(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("GET", "/task", nil)
	w := httptest.NewRecorder()
	agent.handleTask(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleTask_InvalidJSON(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("POST", "/task", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	agent.handleTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTask_ProjectMismatch(t *testing.T) {
	agent := newTestAgent(t)

	body, _ := json.Marshal(TaskRequest{ProjectID: "wrong-project", Action: "bash"})
	req := httptest.NewRequest("POST", "/task", bytes.NewReader(body))
	w := httptest.NewRecorder()
	agent.handleTask(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleTask_Accepted(t *testing.T) {
	agent := newTestAgent(t)

	body, _ := json.Marshal(TaskRequest{
		TaskID:    "task-1",
		BeadID:    "bead-1",
		Action:    "scope",
		ProjectID: "test-proj",
	})
	req := httptest.NewRequest("POST", "/task", bytes.NewReader(body))
	w := httptest.NewRecorder()
	agent.handleTask(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "accepted" {
		t.Errorf("expected accepted, got %q", resp["status"])
	}
}

func TestHandleExec_MethodNotAllowed(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("GET", "/exec", nil)
	w := httptest.NewRecorder()
	agent.handleExec(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleExec_InvalidJSON(t *testing.T) {
	agent := newTestAgent(t)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader([]byte("{invalid")))
	w := httptest.NewRecorder()
	agent.handleExec(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleExec_MissingCommand(t *testing.T) {
	agent := newTestAgent(t)

	body, _ := json.Marshal(map[string]string{"command": ""})
	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	agent.handleExec(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleExec_Success(t *testing.T) {
	agent := newTestAgent(t)

	body, _ := json.Marshal(map[string]interface{}{
		"command": "echo hello",
		"timeout": 5,
	})
	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	agent.handleExec(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["success"] != true {
		t.Errorf("expected success, got %v", resp)
	}
	if resp["exit_code"].(float64) != 0 {
		t.Errorf("expected exit 0, got %v", resp["exit_code"])
	}
}

func TestHandleExec_FailedCommand(t *testing.T) {
	agent := newTestAgent(t)

	body, _ := json.Marshal(map[string]interface{}{
		"command": "false",
	})
	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	agent.handleExec(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["success"] != false {
		t.Error("expected failure")
	}
}

func TestRegisterHandlers(t *testing.T) {
	agent := newTestAgent(t)
	mux := http.NewServeMux()
	agent.RegisterHandlers(mux)

	// Verify handlers are registered by making test requests
	tests := []struct {
		path   string
		method string
	}{
		{"/health", "GET"},
		{"/status", "GET"},
	}

	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("handler not registered for %s %s", tc.method, tc.path)
		}
	}
}

func TestExecuteBash(t *testing.T) {
	agent := newTestAgent(t)

	output, err := agent.executeBash(context.Background(), map[string]interface{}{"command": "echo test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "test\n" {
		t.Errorf("expected 'test\\n', got %q", output)
	}
}

func TestExecuteBash_MissingCommand(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeBash(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing command")
	}
}

func TestExecuteRead(t *testing.T) {
	agent := newTestAgent(t)

	testFile := filepath.Join(agent.config.WorkDir, "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	output, err := agent.executeRead(context.Background(), map[string]interface{}{"path": "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "hello world" {
		t.Errorf("expected 'hello world', got %q", output)
	}
}

func TestExecuteRead_MissingPath(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeRead(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestExecuteWrite(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeWrite(context.Background(), map[string]interface{}{
		"path":    "output.txt",
		"content": "written content",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agent.config.WorkDir, "output.txt"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(data) != "written content" {
		t.Errorf("expected 'written content', got %q", string(data))
	}
}

func TestExecuteWrite_MissingPath(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeWrite(context.Background(), map[string]interface{}{"content": "x"})
	if err == nil {
		t.Error("expected error for missing path")
	}
}

func TestExecuteWrite_MissingContent(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeWrite(context.Background(), map[string]interface{}{"path": "x.txt"})
	if err == nil {
		t.Error("expected error for missing content")
	}
}

func TestExecuteScope(t *testing.T) {
	agent := newTestAgent(t)

	// Create a file so ls has something to show
	os.WriteFile(filepath.Join(agent.config.WorkDir, "file.txt"), []byte("x"), 0644)

	output, err := agent.executeScope(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestExecuteScope_CustomPath(t *testing.T) {
	agent := newTestAgent(t)

	subdir := filepath.Join(agent.config.WorkDir, "subdir")
	os.MkdirAll(subdir, 0755)
	os.WriteFile(filepath.Join(subdir, "sub.txt"), []byte("x"), 0644)

	output, err := agent.executeScope(context.Background(), map[string]interface{}{"path": "subdir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestExecuteGitCommit_NotGitRepo(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeGitCommit(context.Background(), map[string]interface{}{"message": "test"})
	if err == nil {
		t.Error("expected error in non-git directory")
	}
}

func TestExecuteGitCommit_MissingMessage(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeGitCommit(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing message")
	}
}

func TestExecuteGitPush_NotGitRepo(t *testing.T) {
	agent := newTestAgent(t)

	_, err := agent.executeGitPush(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Error("expected error in non-git directory")
	}
}

func TestHandleStatus_Busy(t *testing.T) {
	agent := newTestAgent(t)
	agent.currentTask = &TaskExecution{
		Request:   &TaskRequest{TaskID: "t1", BeadID: "b1", Action: "bash"},
		StartTime: time.Now(),
	}

	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()
	agent.handleStatus(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	if body["busy"] != true {
		t.Error("agent should be busy")
	}
	ct := body["current_task"].(map[string]interface{})
	if ct["task_id"] != "t1" {
		t.Errorf("got task_id %v", ct["task_id"])
	}
}

func TestReadFileContent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(path, []byte("content here"), 0644)

	content, err := readFileContent(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "content here" {
		t.Errorf("got %q", content)
	}
}

func TestReadFileContent_NotFound(t *testing.T) {
	_, err := readFileContent("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestExecuteTask_BashAction(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t1",
		BeadID:    "b1",
		ProjectID: "test-proj",
		Action:    "bash",
		Params:    map[string]interface{}{"command": "echo hello"},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if !result.Success {
			t.Errorf("expected success, got error: %s", result.Error)
		}
		if result.Output != "hello\n" {
			t.Errorf("expected 'hello\\n', got %q", result.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for task result")
	}
}

func TestExecuteTask_ReadAction(t *testing.T) {
	agent := newTestAgent(t)

	testFile := filepath.Join(agent.config.WorkDir, "read_me.txt")
	os.WriteFile(testFile, []byte("read this"), 0644)

	req := &TaskRequest{
		TaskID:    "t2",
		BeadID:    "b2",
		ProjectID: "test-proj",
		Action:    "read",
		Params:    map[string]interface{}{"path": "read_me.txt"},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if !result.Success {
			t.Errorf("expected success: %s", result.Error)
		}
		if result.Output != "read this" {
			t.Errorf("expected 'read this', got %q", result.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_WriteAction(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t3",
		BeadID:    "b3",
		ProjectID: "test-proj",
		Action:    "write",
		Params:    map[string]interface{}{"path": "out.txt", "content": "new data"},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if !result.Success {
			t.Errorf("expected success: %s", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	data, _ := os.ReadFile(filepath.Join(agent.config.WorkDir, "out.txt"))
	if string(data) != "new data" {
		t.Errorf("file content mismatch: %q", string(data))
	}
}

func TestExecuteTask_ScopeAction(t *testing.T) {
	agent := newTestAgent(t)
	os.WriteFile(filepath.Join(agent.config.WorkDir, "afile.txt"), []byte("x"), 0644)

	req := &TaskRequest{
		TaskID:    "t4",
		BeadID:    "b4",
		ProjectID: "test-proj",
		Action:    "scope",
		Params:    map[string]interface{}{},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if !result.Success {
			t.Errorf("expected success: %s", result.Error)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_UnsupportedAction(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t5",
		BeadID:    "b5",
		ProjectID: "test-proj",
		Action:    "unknown_action",
		Params:    map[string]interface{}{},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if result.Success {
			t.Error("expected failure for unsupported action")
		}
		if result.Error == "" {
			t.Error("expected error message")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_FailedBash(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t6",
		BeadID:    "b6",
		ProjectID: "test-proj",
		Action:    "bash",
		Params:    map[string]interface{}{"command": "exit 1"},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if result.Success {
			t.Error("expected failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_GitCommitAction(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t7",
		BeadID:    "b7",
		ProjectID: "test-proj",
		Action:    "git_commit",
		Params:    map[string]interface{}{"message": "test commit"},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		// Will fail because work dir is not a git repo, but the action path is exercised
		if result.TaskID != "t7" {
			t.Errorf("expected task t7, got %q", result.TaskID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_GitPushAction(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t8",
		BeadID:    "b8",
		ProjectID: "test-proj",
		Action:    "git_push",
		Params:    map[string]interface{}{},
	}

	agent.executeTask(req)

	select {
	case result := <-agent.taskResultCh:
		if result.TaskID != "t8" {
			t.Errorf("expected task t8, got %q", result.TaskID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestExecuteTask_ClearsCurrentTask(t *testing.T) {
	agent := newTestAgent(t)

	req := &TaskRequest{
		TaskID:    "t9",
		BeadID:    "b9",
		ProjectID: "test-proj",
		Action:    "scope",
		Params:    map[string]interface{}{},
	}

	agent.executeTask(req)
	<-agent.taskResultCh

	if agent.currentTask != nil {
		t.Error("currentTask should be nil after execution")
	}
}
