package formats_test

import (
	"os"
	"strings"
	"testing"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

func TestJSONFormatter_Detect(t *testing.T) {
	f := &formats.JSONFormatter{}
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"valid object", `{"key": "value"}`, true},
		{"valid array", `[1, 2, 3]`, true},
		{"valid nested", `{"a": {"b": [1,2]}}`, true},
		{"with leading whitespace", "  \n{\"x\": 1}", true},
		{"invalid json", `{not valid json`, false},
		{"plain text", "hello world", false},
		{"empty", "", false},
		{"starts with [ but invalid", "[oops", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := f.Detect([]byte(tc.output), "")
			if got != tc.want {
				t.Errorf("Detect() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestJSONFormatter_Summarize_Object(t *testing.T) {
	data, err := os.ReadFile("../testdata/acli_page.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.JSONFormatter{}
	s := f.Summarize(data)

	if s.Format != "json" {
		t.Errorf("Format = %q, want json", s.Format)
	}
	if !strings.Contains(s.Text, "JSON object") {
		t.Errorf("expected 'JSON object' in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "Top-level keys") {
		t.Errorf("expected 'Top-level keys' in summary, got: %s", s.Text)
	}
	if s.TotalBytes != len(data) {
		t.Errorf("TotalBytes = %d, want %d", s.TotalBytes, len(data))
	}
}

func TestJSONFormatter_Summarize_Array(t *testing.T) {
	data, err := os.ReadFile("../testdata/kubectl_pods.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &formats.JSONFormatter{}
	s := f.Summarize(data)

	if !strings.Contains(s.Text, "JSON array") {
		t.Errorf("expected 'JSON array' in summary, got: %s", s.Text)
	}
	if !strings.Contains(s.Text, "4 items") {
		t.Errorf("expected '4 items' in summary, got: %s", s.Text)
	}
}

func TestJSONFormatter_Summarize_Empty(t *testing.T) {
	f := &formats.JSONFormatter{}
	// Empty should not be detected, but Summarize should handle gracefully.
	s := f.Summarize([]byte("{}"))
	if s.Format != "json" {
		t.Errorf("Format = %q, want json", s.Format)
	}
}
