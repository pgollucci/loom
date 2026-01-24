package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type staticResolver struct {
	dir string
}

func (r staticResolver) GetProjectWorkDir(projectID string) string {
	return r.dir
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	mgr := NewManager(staticResolver{dir: dir})
	res, err := mgr.ReadFile(context.Background(), "proj-1", "README.md")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if res.Content != "hello" {
		t.Fatalf("unexpected content: %s", res.Content)
	}
}

func TestReadFilePathTraversal(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(staticResolver{dir: dir})
	if _, err := mgr.ReadFile(context.Background(), "proj-1", "../secret.txt"); err == nil {
		t.Fatalf("expected path traversal error")
	}
}

func TestSearchText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mgr := NewManager(staticResolver{dir: dir})
	results, err := mgr.SearchText(context.Background(), "proj-1", ".", "TODO", 10)
	if err != nil {
		t.Fatalf("search text: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 match, got %d", len(results))
	}
}
