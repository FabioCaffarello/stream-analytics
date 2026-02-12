package replay

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

const maxFixtureLineBytes = 16 * 1024 * 1024

// Reader streams validated fixture records from a JSONL file.
type Reader struct {
	file      *os.File
	scanner   *bufio.Scanner
	lineIndex int
}

// NewReader opens a fixture JSONL reader.
func NewReader(path string) (*Reader, *problem.Problem) {
	if strings.TrimSpace(path) == "" {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "fixture path must not be empty"),
			"field", "path",
		)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, problem.Wrap(err, problem.Internal, "open fixture file failed")
	}

	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), maxFixtureLineBytes)
	return &Reader{file: f, scanner: s}, nil
}

// Next returns the next record, ok=false on EOF.
func (r *Reader) Next() (FixtureRecord, bool, *problem.Problem) {
	if r.scanner == nil {
		return FixtureRecord{}, false, problem.New(problem.ValidationFailed, "fixture reader is closed")
	}
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return FixtureRecord{}, false, problem.Wrap(err, problem.Internal, "scan fixture line failed")
		}
		return FixtureRecord{}, false, nil
	}

	r.lineIndex++
	line := append([]byte(nil), r.scanner.Bytes()...)

	var raw fixtureLine
	if err := json.Unmarshal(line, &raw); err != nil {
		return FixtureRecord{}, false, annotateLine(problem.Wrap(err, problem.ValidationFailed, "invalid fixture line json"), r.lineIndex)
	}

	rec, p := parseFixtureLine(raw, r.lineIndex)
	if p != nil {
		return FixtureRecord{}, false, p
	}
	return rec, true, nil
}

// Close closes the underlying fixture file.
func (r *Reader) Close() *problem.Problem {
	if r == nil || r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	r.scanner = nil
	if err != nil {
		return problem.Wrap(err, problem.Internal, "close fixture reader failed")
	}
	return nil
}

func parseFixtureLine(raw fixtureLine, line int) (FixtureRecord, *problem.Problem) {
	if strings.TrimSpace(raw.Subject) == "" {
		return FixtureRecord{}, annotateLine(problem.WithDetail(
			problem.New(problem.ValidationFailed, "subject must not be empty"),
			"field", "subject",
		), line)
	}
	if strings.TrimSpace(raw.SHA256) == "" {
		return FixtureRecord{}, annotateLine(problem.WithDetail(
			problem.New(problem.ValidationFailed, "sha256 must not be empty"),
			"field", "sha256",
		), line)
	}

	contentType, p := envelope.NormalizeContentType(raw.ContentType)
	if p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}

	env := raw.Envelope
	if len(env.Payload) > 0 {
		return FixtureRecord{}, annotateLine(problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope payload must not be embedded in fixture envelope object"),
			"field", "envelope.payload",
		), line)
	}

	if strings.TrimSpace(env.ContentType) != "" {
		envType, ep := envelope.NormalizeContentType(env.ContentType)
		if ep != nil {
			return FixtureRecord{}, annotateLine(ep, line)
		}
		if envType != contentType {
			return FixtureRecord{}, annotateLine(problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "content_type mismatch envelope=%q line=%q", envType, contentType),
				"field", "content_type",
			), line)
		}
	}
	env.ContentType = contentType

	base := fixtureBase{
		Subject:     raw.Subject,
		Envelope:    env,
		ContentType: contentType,
		PayloadJSON: raw.PayloadJSON,
		PayloadB64:  strings.TrimSpace(raw.PayloadB64),
	}

	switch contentType {
	case envelope.ContentTypeJSON:
		if len(bytes.TrimSpace(raw.PayloadJSON)) == 0 {
			return FixtureRecord{}, annotateLine(problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_json must not be empty for application/json fixture"),
				"field", "payload_json",
			), line)
		}
		if base.PayloadB64 != "" {
			return FixtureRecord{}, annotateLine(problem.New(problem.ValidationFailed, "payload_b64 must be empty for application/json fixture"), line)
		}
	case envelope.ContentTypeProto:
		if base.PayloadB64 == "" {
			return FixtureRecord{}, annotateLine(problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_b64 must not be empty for application/protobuf fixture"),
				"field", "payload_b64",
			), line)
		}
		if len(bytes.TrimSpace(raw.PayloadJSON)) > 0 {
			return FixtureRecord{}, annotateLine(problem.New(problem.ValidationFailed, "payload_json must be empty for application/protobuf fixture"), line)
		}
	default:
		return FixtureRecord{}, annotateLine(problem.Newf(problem.ValidationFailed, "unsupported content_type %q", contentType), line)
	}

	baseCanonical, p := canonicalBaseBytes(base)
	if p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}
	gotSHA := lineSHA256(baseCanonical)
	if !sameSHA256(raw.SHA256, gotSHA) {
		return FixtureRecord{}, annotateLine(problem.WithDetail(
			problem.New(problem.ValidationFailed, "fixture checksum mismatch"),
			"expected_sha256", strings.ToLower(strings.TrimSpace(raw.SHA256)),
		), line)
	}

	var payloadJSON json.RawMessage
	var payloadB64 string
	switch contentType {
	case envelope.ContentTypeJSON:
		payloadJSON, p = canonicalizeJSONRaw(base.PayloadJSON)
		if p != nil {
			return FixtureRecord{}, annotateLine(p, line)
		}
		env.Payload = append([]byte(nil), payloadJSON...)
	case envelope.ContentTypeProto:
		payloadBytes, err := base64.StdEncoding.DecodeString(base.PayloadB64)
		if err != nil {
			return FixtureRecord{}, annotateLine(problem.Wrap(err, problem.ValidationFailed, "invalid payload_b64"), line)
		}
		env.Payload = payloadBytes
		payloadB64 = base.PayloadB64
	}

	if p := env.Validate(); p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}
	if envelope.SubjectFromEnvelope(env) != raw.Subject {
		return FixtureRecord{}, annotateLine(problem.Newf(problem.ValidationFailed, "subject mismatch: line=%q envelope=%q", raw.Subject, envelope.SubjectFromEnvelope(env)), line)
	}

	return FixtureRecord{
		Subject:     raw.Subject,
		Envelope:    env,
		PayloadJSON: payloadJSON,
		PayloadB64:  payloadB64,
		SHA256:      strings.ToLower(strings.TrimSpace(raw.SHA256)),
	}, nil
}

func annotateLine(p *problem.Problem, line int) *problem.Problem {
	if p == nil {
		return nil
	}
	return problem.WithDetail(p, "line", fmt.Sprintf("%d", line))
}
