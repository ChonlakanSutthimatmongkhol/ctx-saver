package tokenize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "english", text: "hello world", want: 2},
		{name: "code", text: "func main() { println(\"hi\") }", want: 9},
		{name: "json", text: `{"status":"ok","count":3}`, want: 9},
		{name: "thai", text: "สวัสดีชาวโลก", want: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Count(tt.text)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
