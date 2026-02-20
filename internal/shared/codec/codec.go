// Package codec provides serialization utilities for event payloads.
package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/market-raccoon/internal/shared/problem"
)

var codecBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Marshal serializes v to bytes using the canonical codec (JSON for now).
func Marshal(v any) ([]byte, *problem.Problem) {
	buf := codecBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// return buffer to pool before returning
		buf.Reset()
		codecBufPool.Put(buf)
		return nil, problem.Wrap(err, problem.Internal,
			fmt.Sprintf("codec: marshal failed: %T", v))
	}
	// copy out bytes since buffer will be reused
	b := append([]byte(nil), buf.Bytes()...)
	// remove trailing newline added by Encoder.Encode
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	buf.Reset()
	codecBufPool.Put(buf)
	return b, nil
}

// AcquireBuffer returns a pooled bytes.Buffer pre-reset for direct encoding.
func AcquireBuffer() *bytes.Buffer {
	buf := codecBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// ReleaseBuffer returns the buffer to the pool after resetting it.
func ReleaseBuffer(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	buf.Reset()
	codecBufPool.Put(buf)
}

// EncodeTo encodes v into the provided buffer using json.Encoder without
// allocating an intermediate []byte. Caller must manage buffer lifecycle.
func EncodeTo(buf *bytes.Buffer, v any) *problem.Problem {
	if buf == nil {
		return problem.New(problem.Internal, "codec: nil buffer")
	}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return problem.Wrap(err, problem.Internal, fmt.Sprintf("codec: encode failed: %T", v))
	}
	// Encoder.Encode appends a newline; caller may trim if needed.
	return nil
}

// Unmarshal deserializes data into out using the canonical codec.
// out must be a non-nil pointer.
func Unmarshal(data []byte, out any) *problem.Problem {
	if err := json.Unmarshal(data, out); err != nil {
		return problem.Wrap(err, problem.Internal, "codec: unmarshal failed")
	}
	return nil
}

// MarshalPayload serializes a typed payload and attaches event_type/version
// context to any error returned. This helper keeps JSON as default runtime path.
func MarshalPayload(eventType string, version int, v any) ([]byte, *problem.Problem) {
	// Use buffer-backed encoding to reduce intermediate allocations.
	buf := AcquireBuffer()
	defer ReleaseBuffer(buf)
	if p := EncodeTo(buf, v); p != nil {
		return nil, problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", eventType),
				"version", version,
			),
			"payload_type", fmt.Sprintf("%T", v),
		)
	}
	// copy out bytes since buffer will be returned to pool
	b := append([]byte(nil), buf.Bytes()...)
	// trim trailing newline from Encoder.Encode if present
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

// UnmarshalPayload deserializes raw bytes and attaches event_type/version/size
// context to any error returned. This helper keeps JSON as default runtime path.
func UnmarshalPayload(eventType string, version int, data []byte, out any) *problem.Problem {
	if p := Unmarshal(data, out); p != nil {
		return problem.WithDetail(
			problem.WithDetail(
				problem.WithDetail(p, "event_type", eventType),
				"version", version,
			),
			"payload_size", len(data),
		)
	}
	return nil
}
