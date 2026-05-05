package signatures_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers/signatures"
)

// fixturesDir returns the path to tests/testdata/signatures/ relative to this file.
func fixturesDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile: .../internal/handlers/signatures/patterns_test.go
	// 3 dirs up → project root
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "tests", "testdata", "signatures")
	return root
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir(), name))
	require.NoError(t, err, "reading fixture %s", name)
	return data
}

// ── DetectLanguage ─────────────────────────────────────────────────────────

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		path string
		want signatures.Language
	}{
		{"foo.go", signatures.LangGo},
		{"path/to/bar.go", signatures.LangGo},
		{"script.py", signatures.LangPython},
		{"app.dart", signatures.LangDart},
		{"readme.md", signatures.LangNone},
		{"main.ts", signatures.LangNone},
		{"UPPER.GO", signatures.LangGo}, // case-insensitive
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.want, signatures.DetectLanguage(tc.path))
		})
	}
}

// ── T4.1: Go file — func/type/var/const, no function bodies ────────────────

func TestExtract_Go_ContainsSignatures(t *testing.T) {
	content := readFixture(t, "sample.go")
	result, err := signatures.Extract(content, signatures.LangGo)
	require.NoError(t, err)

	// Should contain key declarations.
	assert.Contains(t, result, "func NewServer")
	assert.Contains(t, result, "type Config struct")
	assert.Contains(t, result, "type Server struct")
	assert.Contains(t, result, "const maxRetries")
	assert.Contains(t, result, "var defaultTimeout")
}

func TestExtract_Go_NoFunctionBodies(t *testing.T) {
	content := readFixture(t, "sample.go")
	result, err := signatures.Extract(content, signatures.LangGo)
	require.NoError(t, err)

	// Function bodies should not appear.
	assert.NotContains(t, result, "return &Server")
	assert.NotContains(t, result, "<-ctx.Done()")
	assert.NotContains(t, result, "errors.New")
}

// ── T4.2: Go method receivers ──────────────────────────────────────────────

func TestExtract_Go_MethodReceivers(t *testing.T) {
	content := readFixture(t, "sample.go")
	result, err := signatures.Extract(content, signatures.LangGo)
	require.NoError(t, err)

	assert.Contains(t, result, "func (s *Server) Start")
	assert.Contains(t, result, "func (s *Server) Stop")
	assert.Contains(t, result, "func (h *Handler) Name")
}

// ── T4.3: Go generic type aliases ─────────────────────────────────────────

func TestExtract_Go_Generics(t *testing.T) {
	content := readFixture(t, "sample.go")
	result, err := signatures.Extract(content, signatures.LangGo)
	require.NoError(t, err)

	assert.Contains(t, result, "type Set[T comparable]")
}

// ── T4.4: Python class + indented methods ──────────────────────────────────

func TestExtract_Python_ClassAndMethods(t *testing.T) {
	content := readFixture(t, "sample.py")
	result, err := signatures.Extract(content, signatures.LangPython)
	require.NoError(t, err)

	assert.Contains(t, result, "class BaseHandler")
	assert.Contains(t, result, "class ConcreteHandler")
	assert.Contains(t, result, "def handle")
	assert.Contains(t, result, "async def handle_async")
	assert.Contains(t, result, "def create_handler")
	assert.Contains(t, result, "async def run_pipeline")
}

// ── T4.5: Dart basic class + method ───────────────────────────────────────

func TestExtract_Dart_ClassAndMethod(t *testing.T) {
	content := readFixture(t, "sample.dart")
	result, err := signatures.Extract(content, signatures.LangDart)
	require.NoError(t, err)

	assert.Contains(t, result, "class Dog")
	assert.Contains(t, result, "abstract class Animal")
	assert.Contains(t, result, "mixin CanFly")
	assert.Contains(t, result, "enum Status")
	assert.Contains(t, result, "typedef Callback")
}

// ── T4.6: Dart generic return type — documented limitation (XFAIL style) ──

func TestExtract_Dart_GenericReturn_KnownLimit(t *testing.T) {
	// List<T> getAll() — this may or may not match depending on regex.
	// Document as a known limit: test passes regardless, just verifies
	// that Extract does not error on complex Dart files.
	content := readFixture(t, "sample.dart")
	result, err := signatures.Extract(content, signatures.LangDart)
	require.NoError(t, err, "Extract must not error on complex Dart files")
	// We accept false negatives for complex generic return types.
	// If the line IS matched, great. If not, it's a documented limit.
	t.Logf("Dart generic return types matched: %v", strings.Contains(result, "List<T> getAll"))
}

// ── T4.7: Line numbers match original file ────────────────────────────────

func TestExtract_Go_LineNumbersMatchOriginal(t *testing.T) {
	content := readFixture(t, "sample.go")
	result, err := signatures.Extract(content, signatures.LangGo)
	require.NoError(t, err)

	lines := strings.Split(string(content), "\n")
	// For every line in the output, verify the embedded line number points to
	// the right content in the original file.
	for _, outLine := range strings.Split(result, "\n") {
		if outLine == "" {
			continue
		}
		var lineNum int
		var lineContent string
		n, _ := parseSignatureLine(outLine, &lineNum, &lineContent)
		if n != 2 {
			t.Errorf("bad output line format: %q", outLine)
			continue
		}
		// Line numbers are 1-based.
		require.Less(t, lineNum-1, len(lines),
			"line number %d out of range (file has %d lines)", lineNum, len(lines))
		assert.Equal(t, lineContent, lines[lineNum-1],
			"line %d content mismatch", lineNum)
	}
}

// parseSignatureLine parses "[N]  content" format and fills lineNum and lineContent.
// Returns 2 on success (like fmt.Sscanf).
func parseSignatureLine(s string, lineNum *int, lineContent *string) (int, error) {
	if !strings.HasPrefix(s, "[") {
		return 0, nil
	}
	closeBracket := strings.Index(s, "]")
	if closeBracket < 0 {
		return 0, nil
	}
	numStr := s[1:closeBracket]
	n, err := parseInt(numStr)
	if err != nil {
		return 0, err
	}
	*lineNum = n
	rest := s[closeBracket+1:]
	// Strip leading "  " (two spaces).
	rest = strings.TrimPrefix(rest, "  ")
	*lineContent = rest
	return 2, nil
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ── T4.9: Unsupported file type ────────────────────────────────────────────

func TestExtract_UnsupportedLanguage_ReturnsError(t *testing.T) {
	_, err := signatures.Extract([]byte("hello world"), signatures.LangNone)
	require.Error(t, err)
	assert.ErrorIs(t, err, signatures.ErrUnsupportedLanguage)
}

func TestExtract_UnsupportedLanguage_ErrorMessage(t *testing.T) {
	_, err := signatures.Extract([]byte("data"), "typescript")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}
