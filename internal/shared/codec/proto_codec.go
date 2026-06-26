package codec

import (
	"fmt"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"google.golang.org/protobuf/proto"
)

// ProtoCodec is a typed protobuf encoder/decoder for registry usage.
type ProtoCodec[T proto.Message] struct {
	New func() T
}

func (c ProtoCodec[T]) Encode(v any) ([]byte, *problem.Problem) {
	msg, ok := v.(T)
	if !ok {
		var zero T
		return nil, problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "proto codec type mismatch: got %T want %T", v, zero),
			"payload_type", fmt.Sprintf("%T", v),
		)
	}

	data, err := (proto.MarshalOptions{Deterministic: true}).Marshal(msg)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "proto codec: marshal failed")
	}
	return data, nil
}

func (c ProtoCodec[T]) Decode(b []byte) (any, *problem.Problem) {
	if c.New == nil {
		return nil, problem.New(problem.ValidationFailed, "proto codec factory must not be nil")
	}

	msg := c.New()
	if any(msg) == nil {
		return nil, problem.New(problem.ValidationFailed, "proto codec factory returned nil message")
	}

	if err := proto.Unmarshal(b, msg); err != nil {
		return nil, problem.Wrap(err, problem.Internal, "proto codec: unmarshal failed")
	}
	return msg, nil
}
