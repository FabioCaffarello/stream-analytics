package codec

import (
	"strings"
	"sync"

	"github.com/market-raccoon/internal/shared/problem"
)

type Format string

const (
	FormatJSON  Format = "application/json"
	FormatProto Format = "application/protobuf"
)

// SchemaKey identifies a payload schema by type, version and wire format.
type SchemaKey struct {
	Type    string
	Version int32
	Format  Format
}

type Encoder interface {
	Encode(any) ([]byte, *problem.Problem)
}

type Decoder interface {
	Decode([]byte) (any, *problem.Problem)
}

// Registry holds wire codecs indexed by schema key.
type Registry struct {
	mu       sync.RWMutex
	decoders map[SchemaKey]Decoder
	encoders map[SchemaKey]Encoder
}

func NewRegistry() *Registry {
	return &Registry{
		decoders: make(map[SchemaKey]Decoder),
		encoders: make(map[SchemaKey]Encoder),
	}
}

func (r *Registry) Register(key SchemaKey, enc Encoder, dec Decoder) *problem.Problem {
	if r == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	key, p := normalizeSchemaKey(key)
	if p != nil {
		return p
	}
	if enc == nil {
		return problem.New(problem.ValidationFailed, "codec encoder must not be nil")
	}
	if dec == nil {
		return problem.New(problem.ValidationFailed, "codec decoder must not be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.encoders[key]; ok {
		return problem.Newf(problem.Conflict, "encoder already registered for type=%q version=%d format=%q", key.Type, key.Version, key.Format)
	}
	if _, ok := r.decoders[key]; ok {
		return problem.Newf(problem.Conflict, "decoder already registered for type=%q version=%d format=%q", key.Type, key.Version, key.Format)
	}

	r.encoders[key] = enc
	r.decoders[key] = dec
	return nil
}

func (r *Registry) Encoder(key SchemaKey) (Encoder, bool) {
	if r == nil {
		return nil, false
	}
	key, p := normalizeSchemaKey(key)
	if p != nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	enc, ok := r.encoders[key]
	return enc, ok
}

func (r *Registry) Decoder(key SchemaKey) (Decoder, bool) {
	if r == nil {
		return nil, false
	}
	key, p := normalizeSchemaKey(key)
	if p != nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	dec, ok := r.decoders[key]
	return dec, ok
}

func normalizeSchemaKey(key SchemaKey) (SchemaKey, *problem.Problem) {
	key.Type = strings.TrimSpace(key.Type)
	if key.Type == "" {
		return SchemaKey{}, problem.New(problem.ValidationFailed, "schema type must not be empty")
	}
	if key.Version < 1 {
		return SchemaKey{}, problem.Newf(problem.ValidationFailed, "schema version must be >= 1, got %d", key.Version)
	}
	switch key.Format {
	case FormatJSON, FormatProto:
		return key, nil
	default:
		return SchemaKey{}, problem.Newf(problem.ValidationFailed, "unsupported schema format %q", key.Format)
	}
}
