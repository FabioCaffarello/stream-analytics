package replay

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func canonicalizeJSONRaw(raw []byte) (json.RawMessage, *problem.Problem) {
	value, p := parseJSONValue(raw)
	if p != nil {
		return nil, p
	}
	encoded, p := marshalCanonicalJSON(value)
	if p != nil {
		return nil, p
	}
	return json.RawMessage(encoded), nil
}

func parseJSONValue(raw []byte) (any, *problem.Problem) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "json payload must not be empty"),
			"field", "payload_json",
		)
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, problem.Wrap(err, problem.ValidationFailed, "invalid json payload")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, problem.New(problem.ValidationFailed, "json payload contains multiple documents")
	}
	return out, nil
}

func marshalCanonicalJSON(v any) ([]byte, *problem.Problem) {
	var b bytes.Buffer
	if p := writeCanonicalValue(&b, v); p != nil {
		return nil, p
	}
	return b.Bytes(), nil
}

func writeCanonicalValue(b *bytes.Buffer, v any) *problem.Problem {
	if handled, p := writeCanonicalScalar(b, v); handled {
		return p
	}
	if handled, p := writeCanonicalInt(b, v); handled {
		return p
	}
	if handled, p := writeCanonicalUint(b, v); handled {
		return p
	}
	if handled, p := writeCanonicalFloat(b, v); handled {
		return p
	}
	return writeCanonicalComposite(b, v)
}

func writeCanonicalScalar(b *bytes.Buffer, v any) (bool, *problem.Problem) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
		return true, nil
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return true, nil
	case string:
		encoded, err := json.Marshal(x)
		if err != nil {
			return true, problem.Wrap(err, problem.Internal, "canonical json: string marshal failed")
		}
		b.Write(encoded)
		return true, nil
	case json.Number:
		n := strings.TrimSpace(x.String())
		if !json.Valid([]byte(n)) {
			return true, problem.Newf(problem.ValidationFailed, "invalid json number %q", n)
		}
		b.WriteString(n)
		return true, nil
	default:
		return false, nil
	}
}

func writeCanonicalInt(b *bytes.Buffer, v any) (bool, *problem.Problem) {
	switch x := v.(type) {
	case int:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int16:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	default:
		return false, nil
	}
	return true, nil
}

func writeCanonicalUint(b *bytes.Buffer, v any) (bool, *problem.Problem) {
	switch x := v.(type) {
	case uint:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint8:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint16:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	default:
		return false, nil
	}
	return true, nil
}

func writeCanonicalFloat(b *bytes.Buffer, v any) (bool, *problem.Problem) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return true, problem.New(problem.ValidationFailed, "invalid float value for canonical json")
		}
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case float32:
		fx := float64(x)
		if math.IsNaN(fx) || math.IsInf(fx, 0) {
			return true, problem.New(problem.ValidationFailed, "invalid float value for canonical json")
		}
		b.WriteString(strconv.FormatFloat(fx, 'g', -1, 32))
	default:
		return false, nil
	}
	return true, nil
}

func writeCanonicalComposite(b *bytes.Buffer, v any) *problem.Problem {
	switch x := v.(type) {
	case []any:
		return writeCanonicalArray(b, x)
	case map[string]any:
		return writeCanonicalObject(b, x)
	case json.RawMessage:
		parsed, p := parseJSONValue(x)
		if p != nil {
			return p
		}
		return writeCanonicalValue(b, parsed)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return problem.Wrap(err, problem.ValidationFailed, "canonical json: unsupported value")
		}
		parsed, p := parseJSONValue(encoded)
		if p != nil {
			return p
		}
		return writeCanonicalValue(b, parsed)
	}
}

func writeCanonicalArray(b *bytes.Buffer, values []any) *problem.Problem {
	b.WriteByte('[')
	for i := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		if p := writeCanonicalValue(b, values[i]); p != nil {
			return p
		}
	}
	b.WriteByte(']')
	return nil
}

func writeCanonicalObject(b *bytes.Buffer, obj map[string]any) *problem.Problem {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteByte('{')
	for i := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		key := keys[i]
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return problem.Wrap(err, problem.Internal, "canonical json: key marshal failed")
		}
		b.Write(keyJSON)
		b.WriteByte(':')
		if p := writeCanonicalValue(b, obj[key]); p != nil {
			return p
		}
	}
	b.WriteByte('}')
	return nil
}

func canonicalEnvelopeMap(env envelope.Envelope) map[string]any {
	m := map[string]any{
		"type":            env.Type,
		"version":         env.Version,
		"venue":           env.Venue,
		"instrument":      env.Instrument,
		"ts_exchange":     env.TsExchange,
		"ts_ingest":       env.TsIngest,
		"seq":             env.Seq,
		"idempotency_key": env.IdempotencyKey,
		"content_type":    env.ContentType,
	}
	if len(env.Meta) > 0 {
		meta := make(map[string]any, len(env.Meta))
		for k, v := range env.Meta {
			meta[k] = v
		}
		m["meta"] = meta
	}
	return m
}

func canonicalBaseBytes(base fixtureBase) ([]byte, *problem.Problem) {
	obj := map[string]any{
		"subject":      base.Subject,
		"envelope":     canonicalEnvelopeMap(base.Envelope),
		"content_type": base.ContentType,
	}

	switch base.ContentType {
	case envelope.ContentTypeJSON:
		if len(bytes.TrimSpace(base.PayloadJSON)) == 0 {
			return nil, problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_json must not be empty"),
				"field", "payload_json",
			)
		}
		payload, p := parseJSONValue(base.PayloadJSON)
		if p != nil {
			return nil, p
		}
		obj["payload_json"] = payload
	case envelope.ContentTypeProto:
		if strings.TrimSpace(base.PayloadB64) == "" {
			return nil, problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_b64 must not be empty"),
				"field", "payload_b64",
			)
		}
		obj["payload_b64"] = base.PayloadB64
	default:
		return nil, problem.Newf(problem.ValidationFailed, "unsupported content_type %q", base.ContentType)
	}

	return marshalCanonicalJSON(obj)
}

func canonicalLineBytes(base fixtureBase, sha string) ([]byte, *problem.Problem) {
	if strings.TrimSpace(sha) == "" {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "sha256 must not be empty"),
			"field", "sha256",
		)
	}

	obj := map[string]any{
		"subject":      base.Subject,
		"envelope":     canonicalEnvelopeMap(base.Envelope),
		"content_type": base.ContentType,
		"sha256":       strings.ToLower(strings.TrimSpace(sha)),
	}

	switch base.ContentType {
	case envelope.ContentTypeJSON:
		payload, p := parseJSONValue(base.PayloadJSON)
		if p != nil {
			return nil, p
		}
		obj["payload_json"] = payload
	case envelope.ContentTypeProto:
		obj["payload_b64"] = base.PayloadB64
	default:
		return nil, problem.Newf(problem.ValidationFailed, "unsupported content_type %q", base.ContentType)
	}

	return marshalCanonicalJSON(obj)
}
