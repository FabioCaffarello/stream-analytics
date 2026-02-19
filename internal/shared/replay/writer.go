package replay

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

const defaultFlushEvery = 1

type writerOptions struct {
	flushEvery int
}

// WriterOption configures replay fixture writer behavior.
type WriterOption func(*writerOptions)

// WithFlushEvery flushes the underlying buffer every n appends.
// Values <= 0 are ignored and default behavior is kept.
func WithFlushEvery(n int) WriterOption {
	return func(o *writerOptions) {
		if n > 0 {
			o.flushEvery = n
		}
	}
}

// Writer appends deterministic replay fixture records to JSONL files.
type Writer struct {
	mu         sync.Mutex
	file       *os.File
	writer     *bufio.Writer
	flushEvery int
	pending    int
	closed     bool
}

// NewWriter creates a fixture writer at path in append-only mode.
func NewWriter(path string, opts ...WriterOption) (*Writer, *problem.Problem) {
	if strings.TrimSpace(path) == "" {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "fixture path must not be empty"),
			"field", "path",
		)
	}

	cfg := writerOptions{flushEvery: defaultFlushEvery}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.flushEvery <= 0 {
		cfg.flushEvery = defaultFlushEvery
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, problem.Wrap(err, problem.Internal, "create fixture directory failed")
		}
	}

	// #nosec G304 -- fixture path is runtime-provided by explicit operator opt-in.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "open fixture file failed")
	}

	return &Writer{
		file:       f,
		writer:     bufio.NewWriter(f),
		flushEvery: cfg.flushEvery,
	}, nil
}

// Append writes one canonical JSONL record.
func (w *Writer) Append(env envelope.Envelope) *problem.Problem {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return problem.New(problem.ValidationFailed, "fixture writer is closed")
	}

	base, p := makeFixtureBaseFromEnvelope(env)
	if p != nil {
		return p
	}

	baseBytes, p := canonicalBaseBytes(base)
	if p != nil {
		return p
	}
	sha := lineSHA256(baseBytes)

	line, p := canonicalLineBytes(base, sha)
	if p != nil {
		return p
	}

	if _, err := w.writer.Write(line); err != nil {
		return problem.Wrap(err, problem.Internal, "write fixture line failed")
	}
	if err := w.writer.WriteByte('\n'); err != nil {
		return problem.Wrap(err, problem.Internal, "write fixture newline failed")
	}

	w.pending++
	if w.pending >= w.flushEvery {
		if err := w.writer.Flush(); err != nil {
			return problem.Wrap(err, problem.Internal, "flush fixture writer failed")
		}
		w.pending = 0
	}

	return nil
}

// Close flushes and closes the underlying fixture file.
func (w *Writer) Close() *problem.Problem {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	if w.writer != nil {
		if err := w.writer.Flush(); err != nil {
			return problem.Wrap(err, problem.Internal, "flush fixture writer on close failed")
		}
	}
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return problem.Wrap(err, problem.Internal, "close fixture file failed")
		}
	}

	w.closed = true
	return nil
}

func makeFixtureBaseFromEnvelope(env envelope.Envelope) (fixtureBase, *problem.Problem) {
	if p := env.Validate(); p != nil {
		return fixtureBase{}, p
	}

	contentType, p := envelope.NormalizeContentType(env.ContentType)
	if p != nil {
		return fixtureBase{}, p
	}

	base := fixtureBase{
		Subject:     envelope.SubjectFromEnvelope(env),
		ContentType: contentType,
		Envelope:    env,
	}
	base.Envelope.ContentType = contentType
	base.Envelope.Payload = nil

	switch contentType {
	case envelope.ContentTypeJSON:
		payloadJSON, cp := canonicalizeJSONRaw(env.Payload)
		if cp != nil {
			return fixtureBase{}, cp
		}
		base.PayloadJSON = payloadJSON
	case envelope.ContentTypeProto:
		base.PayloadB64 = base64.StdEncoding.EncodeToString(env.Payload)
	default:
		return fixtureBase{}, problem.Newf(problem.ValidationFailed, "unsupported content_type %q", contentType)
	}

	if strings.TrimSpace(base.Subject) == "" {
		return fixtureBase{}, problem.New(problem.ValidationFailed, "subject must not be empty")
	}
	return base, nil
}

func (w *Writer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return "replay writer(<nil>)"
	}
	return fmt.Sprintf("replay writer(%s)", w.file.Name())
}
