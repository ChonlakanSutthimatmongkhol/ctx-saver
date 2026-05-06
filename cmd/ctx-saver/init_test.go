package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T7: injectKnowledgeReference is idempotent — running twice must not duplicate.
func TestInjectKnowledgeReference_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("first inject: %v", err)
	}
	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("second inject: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	count := strings.Count(content, "project-knowledge.md")
	if count != 1 {
		t.Errorf("expected exactly 1 occurrence of 'project-knowledge.md', got %d\ncontent:\n%s", count, content)
	}
}

// T8: injectKnowledgeReference is a no-op when file doesn't exist.
func TestInjectKnowledgeReference_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatalf("expected nil for missing file, got: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should not have been created")
	}
}

// Extra: injectKnowledgeReference appends the reference line.
func TestInjectKnowledgeReference_AppendsLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My project\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := injectKnowledgeReference(path); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "project-knowledge.md") {
		t.Error("reference line not appended")
	}
}
