package formats

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFormatters_Summarize(t *testing.T) {
	tests := []struct {
		name       string
		formatter  Formatter
		command    string
		wantText   []string
		wantFormat string
	}{
		{
			name: "build_log", formatter: &BuildLogFormatter{}, command: "xcodebuild -scheme Runner build",
			wantText:   []string{"## Result", "Build failed", "## Errors", "AppDelegate.swift:58:9: error", "1 warning(s)"},
			wantFormat: "build_log",
		},
		{
			name: "pytest", formatter: &PytestFormatter{}, command: "pytest -q",
			wantText:   []string{"2 passed, 1 failed", "tests/test_math.py::test_divide_zero"},
			wantFormat: "pytest",
		},
		{
			name: "jest", formatter: &JestFormatter{}, command: "npm test -- --runInBand",
			wantText:   []string{"Suites: 2 passed, 1 failed", "Tests: 8 passed, 1 failed", "sum › rejects an invalid total"},
			wantFormat: "jest",
		},
		{
			name: "container_log", formatter: &ContainerLogFormatter{}, command: "kubectl logs deployment/api",
			wantText:   []string{"## Errors", "WARN retrying", "ERROR request failed", "## Timeline", "## Panic"},
			wantFormat: "container_log",
		},
		{
			name: "lint", formatter: &LintFormatter{}, command: "golangci-lint run",
			wantText:   []string{"3 issue(s)", "sqlite.go:215:2", "(staticcheck)"},
			wantFormat: "lint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := readFormatterFixture(t, tt.name)
			require.True(t, tt.formatter.Detect(output, tt.command))
			got := tt.formatter.Summarize(output)
			assert.Equal(t, tt.wantFormat, got.Format)
			assert.Positive(t, got.TotalLines)
			assert.Equal(t, len(output), got.TotalBytes)
			for _, want := range tt.wantText {
				assert.Contains(t, got.Text, want)
			}
		})
	}
}

func TestNewFormatters_CrossDetection(t *testing.T) {
	formatters := []Formatter{
		&FlutterTestFormatter{},
		&GoTestFormatter{},
		&PytestFormatter{},
		&JestFormatter{},
		&ContainerLogFormatter{},
		&LintFormatter{},
		&BuildLogFormatter{},
		&JSONFormatter{},
		&GitLogFormatter{},
	}
	fixtures := []struct {
		name    string
		command string
		want    string
	}{
		{"build_log", "xcodebuild -scheme Runner build", "build_log"},
		{"pytest", "pytest -q", "pytest"},
		{"jest", "npm test -- --runInBand", "jest"},
		{"container_log", "docker logs api", "container_log"},
		{"lint", "golangci-lint run", "lint"},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			output := readFormatterFixture(t, fixture.name)
			var detected []string
			for _, formatter := range formatters {
				if formatter.Detect(output, fixture.command) {
					detected = append(detected, formatter.Name())
				}
			}
			assert.Equal(t, []string{fixture.want}, detected)
		})
	}
}

func readFormatterFixture(t *testing.T, name string) []byte {
	t.Helper()
	output, err := os.ReadFile("../testdata/" + name + "_sample.txt")
	require.NoError(t, err)
	return output
}
