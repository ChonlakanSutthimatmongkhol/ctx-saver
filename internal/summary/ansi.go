package summary

import (
	"bytes"
	"regexp"
)

var ansiPattern = regexp.MustCompile(
	"\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07]*(\x07|\x1b\\\\)",
)

// StripANSI removes terminal CSI and OSC escape sequences. Clean input is
// returned unchanged so the common path does not allocate.
func StripANSI(input []byte) []byte {
	if bytes.IndexByte(input, 0x1b) < 0 {
		return input
	}
	return ansiPattern.ReplaceAll(input, nil)
}
