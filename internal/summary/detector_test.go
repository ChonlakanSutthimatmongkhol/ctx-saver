package summary_test

import (
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

func TestDetect_FlutterTest(t *testing.T) {
	output := []byte("Running flutter test...\nAll tests passed!")
	f := summary.Detect(output, "flutter test")
	if f.Name() != "flutter_test" {
		t.Errorf("Name() = %q, want flutter_test", f.Name())
	}
}

func TestDetect_GoTest(t *testing.T) {
	output := []byte("=== RUN   TestFoo\n--- PASS: TestFoo (0.01s)\nok  \tpkg\t0.01s")
	f := summary.Detect(output, "go test ./...")
	if f.Name() != "go_test" {
		t.Errorf("Name() = %q, want go_test", f.Name())
	}
}

func TestDetect_JSON(t *testing.T) {
	output := []byte(`{"key": "value"}`)
	f := summary.Detect(output, "curl https://api.example.com")
	if f.Name() != "json" {
		t.Errorf("Name() = %q, want json", f.Name())
	}
}

func TestDetect_GitLog(t *testing.T) {
	output := []byte("commit abc1234def5678\nAuthor: Alice <a@b.com>")
	f := summary.Detect(output, "git log -10")
	if f.Name() != "git_log" {
		t.Errorf("Name() = %q, want git_log", f.Name())
	}
}

func TestDetect_FallbackGeneric(t *testing.T) {
	output := []byte("some random non-structured output\nline 2\nline 3")
	f := summary.Detect(output, "echo hello")
	if f.Name() != "generic" {
		t.Errorf("Name() = %q, want generic", f.Name())
	}
}

func TestDetectWithConfig_FallbackUsesConfig(t *testing.T) {
	output := []byte("line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10")
	f := summary.DetectWithConfig(output, "echo hello", 3, 2)
	g, ok := f.(*formats.GenericFormatter)
	if !ok {
		t.Fatalf("expected *GenericFormatter, got %T", f)
	}
	if g.HeadLines != 3 || g.TailLines != 2 {
		t.Errorf("HeadLines=%d TailLines=%d, want 3/2", g.HeadLines, g.TailLines)
	}
}

func TestDetect_SummaryHasTotalBytes(t *testing.T) {
	output := []byte("some random plain text output here")
	f := summary.Detect(output, "cat file.txt")
	s := f.Summarize(output)
	if s.TotalBytes != len(output) {
		t.Errorf("TotalBytes = %d, want %d", s.TotalBytes, len(output))
	}
	if !strings.Contains(s.Text, "some random") {
		t.Errorf("expected output text in summary, got: %s", s.Text)
	}
}
