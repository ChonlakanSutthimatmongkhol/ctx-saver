package handlers

import (
	"log/slog"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/tokenize"
)

func setTokenMetrics(out *store.Output, response string) {
	rawTokens, err := tokenize.Count(out.FullOutput)
	if err != nil {
		slog.Warn("tokenizing raw output failed", "error", err)
		return
	}
	responseTokens, err := tokenize.Count(response)
	if err != nil {
		slog.Warn("tokenizing response failed", "error", err)
		return
	}
	out.RawTokens = int64(rawTokens)
	out.ResponseTokens = int64(responseTokens)
	out.ResponseBytes = int64(len(response))
	out.Tokenizer = tokenize.Encoding
}
