package codec

import (
	"encoding/json"
	"fmt"

	"github.com/market-raccoon/internal/shared/problem"
)

// JSONCodec is a typed JSON encoder/decoder for registry usage.
type JSONCodec[T any] struct{}

func (c JSONCodec[T]) Encode(v any) ([]byte, *problem.Problem) {
	typed, ok := v.(T)
	if !ok {
		var zero T
		return nil, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "json codec type mismatch: got %T want %T", v, zero),
			"payload_type", fmt.Sprintf("%T", v),
		)
	}

	data, err := json.Marshal(typed)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "json codec: marshal failed")
	}
	return data, nil
}

func (c JSONCodec[T]) Decode(b []byte) (any, *problem.Problem) {
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "json codec: unmarshal failed")
	}
	return out, nil
}
