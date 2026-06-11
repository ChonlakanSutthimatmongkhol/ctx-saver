// Package tokenize counts model tokens for context-savings metrics.
package tokenize

import (
	"sync"

	"github.com/tiktoken-go/tokenizer"
)

const Encoding = "o200k_base"

var (
	codecOnce sync.Once
	codec     tokenizer.Codec
	codecErr  error
)

// Count returns the number of o200k_base tokens in text.
func Count(text string) (int, error) {
	codecOnce.Do(func() {
		codec, codecErr = tokenizer.Get(tokenizer.O200kBase)
	})
	if codecErr != nil {
		return 0, codecErr
	}
	return codec.Count(text)
}
