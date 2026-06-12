package summary

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripANSI_CSI(t *testing.T) {
	input := []byte("\x1b[32m✓\x1b[0m test passed\n\x1b[31;1mFAIL\x1b[0m")
	assert.Equal(t, "✓ test passed\nFAIL", string(StripANSI(input)))
}

func TestStripANSI_OSC(t *testing.T) {
	input := []byte("\x1b]0;flutter test\x07result\x1b]2;done\x1b\\")
	assert.Equal(t, "result", string(StripANSI(input)))
}

func TestStripANSI_CleanInputReturnsSameSlice(t *testing.T) {
	input := []byte("plain output")
	got := StripANSI(input)
	require.NotEmpty(t, got)
	assert.Equal(t, &input[0], &got[0])
}

func TestStripANSI_EmptyInput(t *testing.T) {
	assert.Empty(t, StripANSI(nil))
}
