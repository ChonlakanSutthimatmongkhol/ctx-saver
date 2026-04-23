package summary

import (
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/summary/formats"
)

// registeredFormatters is the ordered list tried by Detect.
// Order matters: more specific formatters must come before GenericFormatter.
var registeredFormatters = []formats.Formatter{
	&formats.FlutterTestFormatter{},
	&formats.GoTestFormatter{},
	&formats.JSONFormatter{},
	&formats.GitLogFormatter{},
}

// Detect returns the first formatter that matches output/command, or
// GenericFormatter with default settings as fallback.
func Detect(output []byte, command string) formats.Formatter {
	for _, f := range registeredFormatters {
		if f.Detect(output, command) {
			return f
		}
	}
	return &formats.GenericFormatter{HeadLines: 20, TailLines: 5}
}

// DetectWithConfig is like Detect but overrides the generic fallback line counts.
func DetectWithConfig(output []byte, command string, headLines, tailLines int) formats.Formatter {
	for _, f := range registeredFormatters {
		if f.Detect(output, command) {
			return f
		}
	}
	return &formats.GenericFormatter{HeadLines: headLines, TailLines: tailLines}
}
