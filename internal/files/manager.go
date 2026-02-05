package files

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultMaxFileBytes  = 1 << 20 // 1MB
	defaultMaxTreeItems  = 500
	defaultMaxTreeDepth  = 4
	defaultMaxSearchHits = 200
)

type WorkDirResolver interface {
	GetProjectWorkDir(projectID string) string
}

type Manager struct {
	WorkDirs WorkDirResolver
}

type FileResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
}

type TreeEntry struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Depth int    `json:"depth"`
}

type SearchMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type PatchResult struct {
	Applied bool   `json:"applied"`
	Output  string `json:"output,omitempty"`
}

type WriteResult struct {
	Path         string `json:"path"`
	BytesWritten int64  `json:"bytes_written"`
}

func NewManager(resolver WorkDirResolver) *Manager {
	return &Manager{WorkDirs: resolver}
}

func (m *Manager) ReadFile(ctx context.Context, projectID, relPath string) (*FileResult, error) {
	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}
	target, err := safeJoin(workDir, relPath)
	if err != nil {
		return nil, err
	}
	if isBlockedPath(target) {
		return nil, fmt.Errorf("path is not allowed")
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory")
	}
	if info.Size() > defaultMaxFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes limit", defaultMaxFileBytes)
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := readWithLimit(file, defaultMaxFileBytes)
	if err != nil {
		return nil, err
	}
	return &FileResult{
		Path:    relPath,
		Content: content,
		Size:    info.Size(),
	}, nil
}

func (m *Manager) ReadTree(ctx context.Context, projectID, relPath string, maxDepth, limit int) ([]TreeEntry, error) {
	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}
	if relPath == "" {
		relPath = "."
	}
	target, err := safeJoin(workDir, relPath)
	if err != nil {
		return nil, err
	}
	if isBlockedPath(target) {
		return nil, fmt.Errorf("path is not allowed")
	}
	if maxDepth <= 0 {
		maxDepth = defaultMaxTreeDepth
	}
	if limit <= 0 {
		limit = defaultMaxTreeItems
	}

	entries := make([]TreeEntry, 0, limit)
	err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == target {
			return nil
		}
		if isBlockedPath(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(workDir, path)
		if err != nil {
			return err
		}
		depth := depthFromPath(rel)
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		entryType := "file"
		if d.IsDir() {
			entryType = "dir"
		}
		entries = append(entries, TreeEntry{
			Path:  filepath.ToSlash(rel),
			Type:  entryType,
			Depth: depth,
		})
		if len(entries) >= limit {
			return io.EOF
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return nil, err
	}
	return entries, nil
}

func (m *Manager) SearchText(ctx context.Context, projectID, relPath, query string, limit int) ([]SearchMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}
	if relPath == "" {
		relPath = "."
	}
	target, err := safeJoin(workDir, relPath)
	if err != nil {
		return nil, err
	}
	if isBlockedPath(target) {
		return nil, fmt.Errorf("path is not allowed")
	}
	if limit <= 0 {
		limit = defaultMaxSearchHits
	}

	matches := make([]SearchMatch, 0, limit)
	err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if isBlockedPath(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if isBlockedPath(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Size() > defaultMaxFileBytes {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxFileBytes)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			text := scanner.Text()
			if strings.Contains(text, query) {
				rel, err := filepath.Rel(workDir, path)
				if err != nil {
					break
				}
				matches = append(matches, SearchMatch{
					Path: filepath.ToSlash(rel),
					Line: lineNum,
					Text: text,
				})
				if len(matches) >= limit {
					return io.EOF
				}
			}
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return nil, err
	}
	return matches, nil
}

// extractPatchFiles parses a unified diff patch and extracts the file paths
func extractPatchFiles(patch string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	lines := strings.Split(patch, "\n")
	for _, line := range lines {
		// Look for diff headers: "diff --git a/path b/path" or "+++ b/path" or "--- a/path"
		if strings.HasPrefix(line, "diff --git ") {
			// Parse "diff --git a/path b/path"
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Extract "b/path" which is the target file
				path := strings.TrimPrefix(parts[3], "b/")
				if path != "" && !seen[path] {
					files = append(files, path)
					seen[path] = true
				}
			}
		} else if strings.HasPrefix(line, "+++ ") {
			// Parse "+++ b/path" or "+++ /dev/null"
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			path = strings.Fields(path)[0] // Take first field (before any timestamps)
			if path != "" && path != "/dev/null" && !seen[path] {
				files = append(files, path)
				seen[path] = true
			}
		} else if strings.HasPrefix(line, "--- ") {
			// Parse "--- a/path" or "--- /dev/null"
			path := strings.TrimPrefix(line, "--- ")
			path = strings.TrimPrefix(path, "a/")
			path = strings.Fields(path)[0] // Take first field (before any timestamps)
			if path != "" && path != "/dev/null" && !seen[path] {
				files = append(files, path)
				seen[path] = true
			}
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in patch")
	}

	return files, nil
}

func (m *Manager) ApplyPatch(ctx context.Context, projectID, patch string) (*PatchResult, error) {
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}

	// Validate patch size (prevent DoS)
	if len(patch) > 10*1024*1024 { // 10MB limit
		return nil, fmt.Errorf("patch too large (max 10MB)")
	}

	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}

	// Extract and validate all files in the patch
	files, err := extractPatchFiles(patch)
	if err != nil {
		return nil, fmt.Errorf("invalid patch format: %w", err)
	}

	// Validate each file path
	for _, file := range files {
		// Use safeJoin to validate path is within project
		fullPath, err := safeJoin(workDir, file)
		if err != nil {
			return nil, fmt.Errorf("patch modifies unauthorized file: %s (%w)", file, err)
		}

		// Check if path is blocked (e.g., .git, .env)
		if isBlockedPath(fullPath) {
			return nil, fmt.Errorf("patch modifies blocked file: %s", file)
		}

		// Additional sensitive file checks
		lowercaseFile := strings.ToLower(file)
		sensitivePatterns := []string{".env", "secret", "password", "key", "token", "credentials"}
		for _, pattern := range sensitivePatterns {
			if strings.Contains(lowercaseFile, pattern) {
				return nil, fmt.Errorf("patch modifies potentially sensitive file: %s", file)
			}
		}
	}

	// First, check if patch is valid without applying it
	checkCmd := exec.CommandContext(ctx, "git", "apply", "--check", "--whitespace=nowarn", "-")
	checkCmd.Dir = workDir
	checkCmd.Stdin = strings.NewReader(patch)
	var checkOut bytes.Buffer
	checkCmd.Stdout = &checkOut
	checkCmd.Stderr = &checkOut
	if err := checkCmd.Run(); err != nil {
		return &PatchResult{
			Applied: false,
			Output:  fmt.Sprintf("patch validation failed: %s", strings.TrimSpace(checkOut.String())),
		}, fmt.Errorf("patch validation failed: %w", err)
	}

	// Now apply the patch
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "--recount", "-")
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(patch)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return &PatchResult{Applied: false, Output: strings.TrimSpace(out.String())}, err
	}
	return &PatchResult{Applied: true, Output: strings.TrimSpace(out.String())}, nil
}

func (m *Manager) WriteFile(ctx context.Context, projectID, relPath, content string) (*WriteResult, error) {
	if strings.TrimSpace(relPath) == "" {
		return nil, fmt.Errorf("path is required")
	}
	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}
	target, err := safeJoin(workDir, relPath)
	if err != nil {
		return nil, err
	}
	if isBlockedPath(target) {
		return nil, fmt.Errorf("path is not allowed")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file atomically via temp file
	tmpFile, err := os.CreateTemp(dir, ".write-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	n, writeErr := tmpFile.WriteString(content)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write file: %w", writeErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to close file: %w", closeErr)
	}

	// Rename temp file to target (atomic on most filesystems)
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to rename temp file: %w", err)
	}

	return &WriteResult{
		Path:         relPath,
		BytesWritten: int64(n),
	}, nil
}

// MoveFile moves a file from source to target path within the project
func (m *Manager) MoveFile(ctx context.Context, projectID, sourceRelPath, targetRelPath string) error {
	if strings.TrimSpace(sourceRelPath) == "" {
		return fmt.Errorf("source path is required")
	}
	if strings.TrimSpace(targetRelPath) == "" {
		return fmt.Errorf("target path is required")
	}

	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return err
	}

	// Validate source path
	sourcePath, err := safeJoin(workDir, sourceRelPath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if isBlockedPath(sourcePath) {
		return fmt.Errorf("source path is not allowed")
	}

	// Validate target path
	targetPath, err := safeJoin(workDir, targetRelPath)
	if err != nil {
		return fmt.Errorf("invalid target path: %w", err)
	}
	if isBlockedPath(targetPath) {
		return fmt.Errorf("target path is not allowed")
	}

	// Check source exists
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}

	// Ensure target directory exists
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Move file
	if err := os.Rename(sourcePath, targetPath); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}

// DeleteFile deletes a file within the project
func (m *Manager) DeleteFile(ctx context.Context, projectID, relPath string) error {
	if strings.TrimSpace(relPath) == "" {
		return fmt.Errorf("path is required")
	}

	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return err
	}

	// Validate path
	filePath, err := safeJoin(workDir, relPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	if isBlockedPath(filePath) {
		return fmt.Errorf("path is not allowed")
	}

	// Check file exists
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("file not found: %w", err)
	}

	// Delete file
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// RenameFile renames a file within the project
func (m *Manager) RenameFile(ctx context.Context, projectID, sourceRelPath, newName string) error {
	if strings.TrimSpace(sourceRelPath) == "" {
		return fmt.Errorf("source path is required")
	}
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("new name is required")
	}

	// newName should be just the filename, not a path
	if strings.Contains(newName, "/") || strings.Contains(newName, "\\") {
		return fmt.Errorf("new name must be a filename, not a path")
	}

	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return err
	}

	// Validate source path
	sourcePath, err := safeJoin(workDir, sourceRelPath)
	if err != nil {
		return fmt.Errorf("invalid source path: %w", err)
	}
	if isBlockedPath(sourcePath) {
		return fmt.Errorf("source path is not allowed")
	}

	// Check source exists
	if _, err := os.Stat(sourcePath); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}

	// Build target path (same directory, new name)
	targetPath := filepath.Join(filepath.Dir(sourcePath), newName)
	if isBlockedPath(targetPath) {
		return fmt.Errorf("target path is not allowed")
	}

	// Rename file
	if err := os.Rename(sourcePath, targetPath); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

func (m *Manager) resolveWorkDir(projectID string) (string, error) {
	if m.WorkDirs == nil {
		return "", fmt.Errorf("workdir resolver not configured")
	}
	workDir := m.WorkDirs.GetProjectWorkDir(projectID)
	if workDir == "" {
		return "", fmt.Errorf("project workdir not found")
	}
	return filepath.Clean(workDir), nil
}

func safeJoin(base, rel string) (string, error) {
	if rel == "" {
		rel = "."
	}
	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("path must be relative")
	}
	joined := filepath.Join(base, clean)
	baseClean := filepath.Clean(base)
	if joined == baseClean {
		return joined, nil
	}
	if !strings.HasPrefix(joined, baseClean+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes project workdir")
	}
	return joined, nil
}

func isBlockedPath(path string) bool {
	slash := filepath.ToSlash(path)
	if strings.Contains(slash, "/.git/") || strings.HasSuffix(slash, "/.git") {
		return true
	}
	return false
}

func depthFromPath(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func readWithLimit(r io.Reader, limit int64) (string, error) {
	lr := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", fmt.Errorf("file exceeds %d bytes limit", limit)
	}
	return string(data), nil
}
