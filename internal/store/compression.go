package store

import (
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const bodyCompressionThreshold = 4096

var (
	zstdEncoderPool = sync.Pool{
		New: func() any {
			encoder, err := zstd.NewWriter(nil)
			if err != nil {
				panic(fmt.Sprintf("creating zstd encoder: %v", err))
			}
			return encoder
		},
	}
	zstdDecoderPool = sync.Pool{
		New: func() any {
			decoder, err := zstd.NewReader(nil)
			if err != nil {
				panic(fmt.Sprintf("creating zstd decoder: %v", err))
			}
			return decoder
		},
	}
)

func encodeBody(body string) ([]byte, string, error) {
	raw := []byte(body)
	if len(raw) <= bodyCompressionThreshold {
		return raw, "plain", nil
	}
	encoder := zstdEncoderPool.Get().(*zstd.Encoder)
	defer zstdEncoderPool.Put(encoder)
	return encoder.EncodeAll(raw, make([]byte, 0, len(raw)/2)), "zstd", nil
}

func decodeBody(encoding string, raw []byte) ([]byte, error) {
	switch encoding {
	case "", "plain":
		return raw, nil
	case "zstd":
		decoder := zstdDecoderPool.Get().(*zstd.Decoder)
		defer zstdDecoderPool.Put(decoder)
		decoded, err := decoder.DecodeAll(raw, nil)
		if err != nil {
			return nil, fmt.Errorf("decoding zstd body: %w", err)
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported body encoding %q", encoding)
	}
}
