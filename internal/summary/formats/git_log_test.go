package formats_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

func TestGitLogFormatter_Detect(t *testing.T) {
	f := &formats.GitLogFormatter{}
	tests := []struct {
		name    string
		output  string
		command string
		want    bool
	}{
		{"command hint", "", "git log --oneline -10", true},
		{"starts with commit", "commit a1b2c3d4e5f6a7b8c9d0\nAuthor: Alice <a@b.com>", "", true},
		{"contains commit line", "some text\ncommit abc1234\nAuthor: Bob <b@c.com>", "", true},
		{"random text", "hello world this is not git", "", false},
		{"go test output", "--- PASS: TestFoo (0.01s)", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := f.Detect([]byte(tc.output), tc.command)
			if got != tc.want {
				t.Errorf("Detect() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitLogFormatter_Summarize(t *testing.T) {
	data, err := os.ReadFile("../testdata/git_log_linear.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.GitLogFormatter{}
	s := f.Summarize(data)

	if s.Format != "git_log" {
		t.Errorf("Format = %q, want git_log", s.Format)
	}
	if !strings.Contains(s.Text, "5 commits") {
		t.Errorf("expected '5 commits' in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "authors") {
		t.Errorf("expected 'authors' in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "Alice Developer") {
		t.Errorf("expected author name in summary, got: %s", s.Text)
	}
	commitCount, ok := s.Metadata["commit_count"].(int)
	if !ok || commitCount != 5 {
		t.Errorf("commit_count = %v, want 5", s.Metadata["commit_count"])
	}
}

func TestGitLogFormatter_Summarize_Empty(t *testing.T) {
	f := &formats.GitLogFormatter{}
	s := f.Summarize([]byte{})
	if s.Format != "git_log" {
		t.Errorf("Format = %q, want git_log", s.Format)
	}
	if s.Metadata["commit_count"].(int) != 0 {
		t.Errorf("expected 0 commits, got %v", s.Metadata["commit_count"])
	}
}
