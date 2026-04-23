package formats_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

func TestGoTestFormatter_Detect(t *testing.T) {
	f := &formats.GoTestFormatter{}
	tests := []struct {
		name    string
		output  string
		command string
		want    bool
	}{
		{"command hint", "", "go test ./...", true},
		{"ok package line", "ok  \tgithub.com/example/pkg\t0.01s", "", true},
		{"fail package line", "FAIL\tgithub.com/example/pkg\t0.01s", "", true},
		{"run + pass", "=== RUN   TestFoo\n--- PASS: TestFoo (0.01s)", "", true},
		{"run + fail", "=== RUN   TestBar\n--- FAIL: TestBar (0.01s)", "", true},
		{"random text", "hello world random output", "", false},
		{"flutter output", "All tests passed!\n+3: done", "", false},
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

func TestGoTestFormatter_Summarize_Pass(t *testing.T) {
	data, err := os.ReadFile("../testdata/go_test_pass.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.GoTestFormatter{}
	s := f.Summarize(data)

	if s.Format != "go_test" {
		t.Errorf("Format = %q, want go_test", s.Format)
	}
	if !strings.Contains(s.Text, "PASS") {
		t.Errorf("expected PASS in summary, got: %s", s.Text)
	}
	if s.Metadata["pkg_fail"].(int) != 0 {
		t.Errorf("expected pkg_fail=0, got %v", s.Metadata["pkg_fail"])
	}
}

func TestGoTestFormatter_Summarize_Fail(t *testing.T) {
	data, err := os.ReadFile("../testdata/go_test_fail.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.GoTestFormatter{}
	s := f.Summarize(data)

	if !strings.Contains(s.Text, "FAIL") {
		t.Errorf("expected FAIL in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "TestProcessPayment") {
		t.Errorf("expected failed test name in summary, got: %s", s.Text)
	}
	if s.Metadata["pkg_fail"].(int) < 1 {
		t.Errorf("expected pkg_fail >= 1, got %v", s.Metadata["pkg_fail"])
	}
}

func TestGoTestFormatter_Summarize_Coverage(t *testing.T) {
	data, err := os.ReadFile("../testdata/go_test_pass.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.GoTestFormatter{}
	s := f.Summarize(data)
	if !strings.Contains(s.Text, "%") && s.Metadata["coverage"] == "" {
		// Coverage may or may not appear — just ensure no crash
		t.Logf("coverage not found (ok if fixture has none): %s", s.Text)
	}
}

func TestGoTestFormatter_Summarize_Empty(t *testing.T) {
	f := &formats.GoTestFormatter{}
	s := f.Summarize([]byte{})
	if s.Format != "go_test" {
		t.Errorf("Format = %q, want go_test", s.Format)
	}
}
