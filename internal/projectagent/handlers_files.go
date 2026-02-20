package projectagent

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// handleFileWrite writes content to a file in the workspace.
// POST /files/write  {"path": "...", "content": "..."}
func (a *Agent) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	target := filepath.Join(a.config.WorkDir, filepath.Clean(req.Path))
	if !strings.HasPrefix(target, a.config.WorkDir) {
		http.Error(w, "path escapes workspace", http.StatusForbidden)
		return
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("mkdir failed: %v", err),
		})
		return
	}
	if err := os.WriteFile(target, []byte(req.Content), 0644); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("write failed: %v", err),
		})
		return
	}

	log.Printf("[files] wrote %s (%d bytes)", req.Path, len(req.Content))
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"path":          req.Path,
		"bytes_written": len(req.Content),
		"success":       true,
	})
}

// handleFileRead reads a file from the workspace.
// POST /files/read  {"path": "..."}
func (a *Agent) handleFileRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	target := filepath.Join(a.config.WorkDir, filepath.Clean(req.Path))
	if !strings.HasPrefix(target, a.config.WorkDir) {
		http.Error(w, "path escapes workspace", http.StatusForbidden)
		return
	}

	data, err := os.ReadFile(target)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": fmt.Sprintf("read failed: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"path":    req.Path,
		"content": string(data),
		"size":    len(data),
	})
}

// handleFileTree returns a directory listing of the workspace.
// POST /files/tree  {"path": ".", "max_depth": 3}
func (a *Agent) handleFileTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		req.Path = "."
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 4
	}

	root := filepath.Join(a.config.WorkDir, filepath.Clean(req.Path))
	if !strings.HasPrefix(root, a.config.WorkDir) {
		http.Error(w, "path escapes workspace", http.StatusForbidden)
		return
	}

	var entries []string
	baseDepth := strings.Count(root, string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		depth := strings.Count(path, string(os.PathSeparator)) - baseDepth
		if depth > req.MaxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(path, "/.git/") || strings.HasSuffix(path, "/.git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(a.config.WorkDir, path)
		if rel != "." {
			if d.IsDir() {
				entries = append(entries, rel+"/")
			} else {
				entries = append(entries, rel)
			}
		}
		return nil
	})

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"path":    req.Path,
		"entries": entries,
		"count":   len(entries),
	})
}

// handleFileSearch searches for a pattern in workspace files.
// POST /files/search  {"pattern": "...", "glob": "*.go"}
func (a *Agent) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pattern string `json:"pattern"`
		Glob    string `json:"glob"`
		MaxHits int    `json:"max_hits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Pattern == "" {
		http.Error(w, "pattern is required", http.StatusBadRequest)
		return
	}
	if req.MaxHits <= 0 {
		req.MaxHits = 100
	}

	args := []string{"-rn", "--max-count", fmt.Sprintf("%d", req.MaxHits)}
	if req.Glob != "" {
		args = append(args, "--include", req.Glob)
	}
	args = append(args, req.Pattern, ".")

	cmd := exec.Command("grep", args...)
	cmd.Dir = a.config.WorkDir
	out, _ := cmd.Output()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"pattern": req.Pattern,
		"output":  string(out),
	})
}

// handleGitCommit stages all changes and commits.
// POST /git/commit  {"message": "...", "files": ["path1", ...]}
func (a *Agent) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string   `json:"message"`
		Files   []string `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Stage files
	if len(req.Files) > 0 {
		args := append([]string{"add", "--"}, req.Files...)
		addCmd := exec.Command("git", args...)
		addCmd.Dir = a.config.WorkDir
		if out, err := addCmd.CombinedOutput(); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error": fmt.Sprintf("git add failed: %s — %v", out, err),
			})
			return
		}
	} else {
		addCmd := exec.Command("git", "add", "-A")
		addCmd.Dir = a.config.WorkDir
		if out, err := addCmd.CombinedOutput(); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error": fmt.Sprintf("git add -A failed: %s — %v", out, err),
			})
			return
		}
	}

	commitCmd := exec.Command("git", "commit", "-m", req.Message, "--allow-empty-message")
	commitCmd.Dir = a.config.WorkDir
	out, err := commitCmd.CombinedOutput()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": fmt.Sprintf("git commit failed: %s — %v", out, err),
		})
		return
	}

	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = a.config.WorkDir
	sha, _ := shaCmd.Output()

	log.Printf("[git] commit: %s", strings.TrimSpace(string(sha)))
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"commit_sha": strings.TrimSpace(string(sha)),
		"output":     string(out),
		"success":    true,
	})
}

// handleGitPush pushes the current branch to origin.
// POST /git/push  {"branch": "", "set_upstream": false}
func (a *Agent) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Branch      string `json:"branch"`
		SetUpstream bool   `json:"set_upstream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	args := []string{"push"}
	if req.SetUpstream {
		args = append(args, "-u")
	}
	args = append(args, "origin")
	if req.Branch != "" {
		args = append(args, req.Branch)
	} else {
		args = append(args, "HEAD")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = a.config.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error":  fmt.Sprintf("git push failed: %s — %v", out, err),
			"output": string(out),
		})
		return
	}

	log.Printf("[git] pushed to origin")
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"output":  string(out),
	})
}

// handleGitStatus returns git status.
func (a *Agent) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = a.config.WorkDir
	out, err := cmd.Output()
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": string(out),
	})
}

// handleGitDiff returns git diff.
func (a *Agent) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	cmd := exec.Command("git", "diff")
	cmd.Dir = a.config.WorkDir
	out, _ := cmd.Output()

	staged := exec.Command("git", "diff", "--staged")
	staged.Dir = a.config.WorkDir
	stagedOut, _ := staged.Output()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"unstaged": string(out),
		"staged":   string(stagedOut),
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
