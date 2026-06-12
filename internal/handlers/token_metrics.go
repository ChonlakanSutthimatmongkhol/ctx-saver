package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/tokenize"
)

const (
	syncTokenizeLimit  = 2 << 20
	tokenUpdateTimeout = 30 * time.Second
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

// saveOutput stores small-output token metrics synchronously. Large outputs are
// stored immediately with zero metrics, which makes them temporarily visible as
// legacy rows until the detached worker backfills the exact counts.
func saveOutput(ctx context.Context, st store.Store, out *store.Output, response string) error {
	if len(out.FullOutput) <= syncTokenizeLimit {
		setTokenMetrics(out, response)
		return st.Save(ctx, out)
	}

	out.RawTokens = 0
	out.ResponseTokens = 0
	out.ResponseBytes = 0
	out.Tokenizer = ""
	if err := st.Save(ctx, out); err != nil {
		return err
	}

	outputID := out.OutputID
	fullOutput := out.FullOutput
	go backfillTokenMetrics(st, outputID, fullOutput, response)
	return nil
}

func backfillTokenMetrics(st store.Store, outputID, fullOutput, response string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("token metric backfill panicked", "output_id", outputID, "panic", recovered)
		}
	}()

	rawTokens, err := tokenize.Count(fullOutput)
	if err != nil {
		slog.Warn("tokenizing raw output for backfill failed", "output_id", outputID, "error", err)
		return
	}
	responseTokens, err := tokenize.Count(response)
	if err != nil {
		slog.Warn("tokenizing response for backfill failed", "output_id", outputID, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), tokenUpdateTimeout)
	defer cancel()
	if err := st.UpdateTokenMetrics(
		ctx,
		outputID,
		int64(rawTokens),
		int64(responseTokens),
		int64(len(response)),
		tokenize.Encoding,
	); err != nil {
		slog.Warn("backfilling token metrics failed", "output_id", outputID, "error", err)
	}
}
