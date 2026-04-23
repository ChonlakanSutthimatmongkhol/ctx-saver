package formats_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

func TestFlutterFormatter_Detect(t *testing.T) {
	f := &formats.FlutterTestFormatter{}
	tests := []struct {
		name    string
		output  string
		command string
		want    bool
	}{
		{"command hint", "", "flutter test --coverage", true},
		{"all tests passed", "00:04 +8: All tests passed!", "", true},
		{"some tests failed", "00:04 +3 -2: Some tests failed.", "", true},
		{"running flutter test", "Running flutter test...", "", true},
		{"random text", "hello world this is not flutter", "", false},
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

func TestFlutterFormatter_Summarize_Pass(t *testing.T) {
	data, err := os.ReadFile("../testdata/flutter_test_pass.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.FlutterTestFormatter{}
	s := f.Summarize(data)

	if s.Format != "flutter_test" {
		t.Errorf("Format = %q, want flutter_test", s.Format)
	}
	if !strings.Contains(s.Text, "passed") {
		t.Errorf("expected 'passed' in summary text, got: %s", s.Text)
	}
	if s.TotalBytes != len(data) {
		t.Errorf("TotalBytes = %d, want %d", s.TotalBytes, len(data))
	}
	if s.Metadata["failed"].(int) != 0 {
		t.Errorf("expected 0 failed, got %v", s.Metadata["failed"])
	}
}

func TestFlutterFormatter_Summarize_Fail(t *testing.T) {
	data, err := os.ReadFile("../testdata/flutter_test_fail.txt")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.FlutterTestFormatter{}
	s := f.Summarize(data)

	if !strings.Contains(s.Text, "failed") {
		t.Errorf("expected 'failed' in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "ctx_search") {
		t.Errorf("expected ctx_search hint in summary")
	}
	failedTests, ok := s.Metadata["failed_tests"].([]string)
	if !ok || len(failedTests) == 0 {
		t.Errorf("expected failed_tests in metadata, got: %v", s.Metadata["failed_tests"])
	}
}

func TestFlutterFormatter_Summarize_Empty(t *testing.T) {
	f := &formats.FlutterTestFormatter{}
	s := f.Summarize([]byte{})
	if s.TotalBytes != 0 {
		t.Errorf("expected 0 bytes, got %d", s.TotalBytes)
	}
}
