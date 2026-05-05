// Package signatures provides language detection and signature extraction
// for source code files. It supports Go, Python, and Dart (basic regex).
package signatures

import (
	"path/filepath"
	"strings"
)

// Language identifies a supported programming language.
type Language string

const (
	LangGo     Language = "go"
	LangPython Language = "python"
	LangDart   Language = "dart"
	LangNone   Language = ""
)

// DetectLanguage returns the Language for the given file path based on
// its extension. Returns LangNone for unrecognised extensions.
func DetectLanguage(path string) Language {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return LangGo
	case ".py":
		return LangPython
	case ".dart":
		return LangDart
	default:
		return LangNone
	}
}
