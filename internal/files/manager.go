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

func (m *Manager) ApplyPatch(ctx context.Context, projectID, patch string) (*PatchResult, error) {
	if strings.TrimSpace(patch) == "" {
		return nil, fmt.Errorf("patch is required")
	}
	workDir, err := m.resolveWorkDir(projectID)
	if err != nil {
		return nil, err
	}
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
