package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FileWriteResult is the response from the /files/write endpoint.
type FileWriteResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
	Success      bool   `json:"success"`
}

// FileReadResult is the response from the /files/read endpoint.
type FileReadResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int    `json:"size"`
}

// FileTreeResult is the response from the /files/tree endpoint.
type FileTreeResult struct {
	Path    string   `json:"path"`
	Entries []string `json:"entries"`
	Count   int      `json:"count"`
}

// FileSearchResult is the response from the /files/search endpoint.
type FileSearchResult struct {
	Pattern string `json:"pattern"`
	Output  string `json:"output"`
}

// GitCommitResult is the response from the /git/commit endpoint.
type GitCommitResult struct {
	CommitSHA string `json:"commit_sha"`
	Output    string `json:"output"`
	Success   bool   `json:"success"`
}

// GitPushResult is the response from the /git/push endpoint.
type GitPushResult struct {
	Output  string `json:"output"`
	Success bool   `json:"success"`
}

// GitStatusResult is the response from the /git/status endpoint.
type GitStatusResult struct {
	Status string `json:"status"`
}

// GitDiffResult is the response from the /git/diff endpoint.
type GitDiffResult struct {
	Unstaged string `json:"unstaged"`
	Staged   string `json:"staged"`
}

func (c *ProjectAgentClient) postJSON(ctx context.Context, path string, payload interface{}, result interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", errResp.Error)
		}
		return fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}
	if result != nil {
		return json.Unmarshal(respBody, result)
	}
	return nil
}

// WriteFile writes content to a file inside the project container.
func (c *ProjectAgentClient) WriteFile(ctx context.Context, path, content string) (*FileWriteResult, error) {
	var result FileWriteResult
	err := c.postJSON(ctx, "/files/write", map[string]string{
		"path": path, "content": content,
	}, &result)
	return &result, err
}

// ReadFile reads a file from the project container.
func (c *ProjectAgentClient) ReadFile(ctx context.Context, path string) (*FileReadResult, error) {
	var result FileReadResult
	err := c.postJSON(ctx, "/files/read", map[string]string{"path": path}, &result)
	return &result, err
}

// ReadTree returns a directory listing from the project container.
func (c *ProjectAgentClient) ReadTree(ctx context.Context, path string, maxDepth int) (*FileTreeResult, error) {
	var result FileTreeResult
	err := c.postJSON(ctx, "/files/tree", map[string]interface{}{
		"path": path, "max_depth": maxDepth,
	}, &result)
	return &result, err
}

// SearchFiles searches for a pattern in the project container workspace.
func (c *ProjectAgentClient) SearchFiles(ctx context.Context, pattern, glob string, maxHits int) (*FileSearchResult, error) {
	var result FileSearchResult
	err := c.postJSON(ctx, "/files/search", map[string]interface{}{
		"pattern": pattern, "glob": glob, "max_hits": maxHits,
	}, &result)
	return &result, err
}

// GitCommit stages and commits changes inside the project container.
func (c *ProjectAgentClient) GitCommit(ctx context.Context, message string, files []string) (*GitCommitResult, error) {
	var result GitCommitResult
	err := c.postJSON(ctx, "/git/commit", map[string]interface{}{
		"message": message, "files": files,
	}, &result)
	return &result, err
}

// GitPush pushes to remote from inside the project container.
func (c *ProjectAgentClient) GitPush(ctx context.Context, branch string, setUpstream bool) (*GitPushResult, error) {
	var result GitPushResult
	err := c.postJSON(ctx, "/git/push", map[string]interface{}{
		"branch": branch, "set_upstream": setUpstream,
	}, &result)
	return &result, err
}

// GitStatus returns git status from inside the project container.
func (c *ProjectAgentClient) GitStatus(ctx context.Context) (*GitStatusResult, error) {
	var result GitStatusResult
	err := c.postJSON(ctx, "/git/status", map[string]string{}, &result)
	return &result, err
}

// GitDiff returns git diff from inside the project container.
func (c *ProjectAgentClient) GitDiff(ctx context.Context) (*GitDiffResult, error) {
	var result GitDiffResult
	err := c.postJSON(ctx, "/git/diff", map[string]string{}, &result)
	return &result, err
}
