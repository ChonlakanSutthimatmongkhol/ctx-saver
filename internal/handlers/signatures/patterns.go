package signatures

import (
	"errors"
	"regexp"
)

// ErrUnsupportedLanguage is returned by Extract when the language has no
// registered pattern set.
var ErrUnsupportedLanguage = errors.New("signatures: unsupported language")

// patternsByLang maps a Language to its ordered slice of match patterns.
var patternsByLang = map[Language][]*regexp.Regexp{
	LangGo:     goPatterns,
	LangPython: pythonPatterns,
	LangDart:   dartPatterns,
}

// ── Go ──────────────────────────────────────────────────────────────────────

var goPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^package\s+\w+`),
	regexp.MustCompile(`^import\s*\(`),
	regexp.MustCompile(`^func\s+`),    // covers methods: func (r *X) Foo
	regexp.MustCompile(`^type\s+\w+`), // type declarations (struct, interface, alias, generics)
	regexp.MustCompile(`^var\s+`),
	regexp.MustCompile(`^const\s+`),
}

// ── Python ──────────────────────────────────────────────────────────────────

var pythonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(async\s+)?def\s+\w+`),
	regexp.MustCompile(`^class\s+\w+`),
	regexp.MustCompile(`^\s+(async\s+)?def\s+\w+`), // indented class methods
	regexp.MustCompile(`^[A-Z_][A-Z0-9_]*\s*=`),    // constants by convention
	regexp.MustCompile(`^from\s+|^import\s+`),
}

// ── Dart (basic regex) ───────────────────────────────────────────────────────
//
// KNOWN LIMITS:
// - Generic return types with complex nesting (Future<Map<String,List<User>>>)
//   may produce false negatives due to regex line-based approach.
// - Operator overloads (operator ==) may be missed.
// - Multi-line signatures are not detected.
// - Recommendation: use process_script "grep -nE 'class |void |Future |String '"
//   for complex Dart files.

var dartPatterns = []*regexp.Regexp{
	// Top-level declarations
	regexp.MustCompile(`^(abstract\s+)?class\s+\w+`),
	regexp.MustCompile(`^mixin\s+\w+`),
	regexp.MustCompile(`^enum\s+\w+`),
	regexp.MustCompile(`^typedef\s+\w+`),
	// Top-level functions (best-effort: simple return types)
	regexp.MustCompile(`^[\w<>,\s?]+\s+\w+\s*\(`),
	// Indented methods inside class — common case only
	regexp.MustCompile(`^\s+[\w<>,\s?]+\s+\w+\s*\(`),
	// Constructors (named and factory)
	regexp.MustCompile(`^\s+(factory\s+)?\w+\s*\(`),
}
