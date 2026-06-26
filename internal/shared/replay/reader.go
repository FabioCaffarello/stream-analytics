package replay

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
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

	// #nosec G304 -- fixture path is runtime-provided by explicit operator opt-in.
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
	contentType, p := validateFixtureLineHeader(raw)
	if p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}

	env, p := validateFixtureEnvelope(raw.Envelope, contentType)
	if p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}

	base := fixtureBase{
		Subject:     raw.Subject,
		Envelope:    env,
		ContentType: contentType,
		PayloadJSON: raw.PayloadJSON,
		PayloadB64:  strings.TrimSpace(raw.PayloadB64),
	}
	if p := validateFixturePayloadShape(base); p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}
	if p := validateFixtureChecksum(base, raw.SHA256); p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}

	rec, p := decodeFixtureRecord(base, raw.SHA256)
	if p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}
	if p := validateDecodedFixtureSubject(rec); p != nil {
		return FixtureRecord{}, annotateLine(p, line)
	}
	return rec, nil
}

func validateFixtureLineHeader(raw fixtureLine) (string, *problem.Problem) {
	if strings.TrimSpace(raw.Subject) == "" {
		return "", problem.WithDetail(
			problem.New(problem.ValidationFailed, "subject must not be empty"),
			"field", "subject",
		)
	}
	if strings.TrimSpace(raw.SHA256) == "" {
		return "", problem.WithDetail(
			problem.New(problem.ValidationFailed, "sha256 must not be empty"),
			"field", "sha256",
		)
	}
	return envelope.NormalizeContentType(raw.ContentType)
}

func validateFixtureEnvelope(env envelope.Envelope, contentType string) (envelope.Envelope, *problem.Problem) {
	if len(env.Payload) > 0 {
		return envelope.Envelope{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "envelope payload must not be embedded in fixture envelope object"),
			"field", "envelope.payload",
		)
	}
	if strings.TrimSpace(env.ContentType) != "" {
		envType, p := envelope.NormalizeContentType(env.ContentType)
		if p != nil {
			return envelope.Envelope{}, p
		}
		if envType != contentType {
			return envelope.Envelope{}, problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "content_type mismatch envelope=%q line=%q", envType, contentType),
				"field", "content_type",
			)
		}
	}
	env.ContentType = contentType
	return env, nil
}

func validateFixturePayloadShape(base fixtureBase) *problem.Problem {
	switch base.ContentType {
	case envelope.ContentTypeJSON:
		if len(bytes.TrimSpace(base.PayloadJSON)) == 0 {
			return problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_json must not be empty for application/json fixture"),
				"field", "payload_json",
			)
		}
		if base.PayloadB64 != "" {
			return problem.New(problem.ValidationFailed, "payload_b64 must be empty for application/json fixture")
		}
	case envelope.ContentTypeProto:
		if base.PayloadB64 == "" {
			return problem.WithDetail(
				problem.New(problem.ValidationFailed, "payload_b64 must not be empty for application/protobuf fixture"),
				"field", "payload_b64",
			)
		}
		if len(bytes.TrimSpace(base.PayloadJSON)) > 0 {
			return problem.New(problem.ValidationFailed, "payload_json must be empty for application/protobuf fixture")
		}
	default:
		return problem.Newf(problem.ValidationFailed, "unsupported content_type %q", base.ContentType)
	}
	return nil
}

func validateFixtureChecksum(base fixtureBase, expectedSHA string) *problem.Problem {
	baseCanonical, p := canonicalBaseBytes(base)
	if p != nil {
		return p
	}
	gotSHA := lineSHA256(baseCanonical)
	if sameSHA256(expectedSHA, gotSHA) {
		return nil
	}
	return problem.WithDetail(
		problem.New(problem.ValidationFailed, "fixture checksum mismatch"),
		"expected_sha256", strings.ToLower(strings.TrimSpace(expectedSHA)),
	)
}

func decodeFixtureRecord(base fixtureBase, expectedSHA string) (FixtureRecord, *problem.Problem) {
	rec := FixtureRecord{
		Subject: base.Subject,
		Envelope: envelope.Envelope{
			Type:           base.Envelope.Type,
			Version:        base.Envelope.Version,
			Venue:          base.Envelope.Venue,
			Instrument:     base.Envelope.Instrument,
			TsExchange:     base.Envelope.TsExchange,
			TsIngest:       base.Envelope.TsIngest,
			Seq:            base.Envelope.Seq,
			IdempotencyKey: base.Envelope.IdempotencyKey,
			ContentType:    base.Envelope.ContentType,
			Meta:           base.Envelope.Meta,
		},
		SHA256: strings.ToLower(strings.TrimSpace(expectedSHA)),
	}

	switch base.ContentType {
	case envelope.ContentTypeJSON:
		payloadJSON, p := canonicalizeJSONRaw(base.PayloadJSON)
		if p != nil {
			return FixtureRecord{}, p
		}
		rec.PayloadJSON = payloadJSON
		rec.Envelope.Payload = append([]byte(nil), payloadJSON...)
	case envelope.ContentTypeProto:
		payloadBytes, err := base64.StdEncoding.DecodeString(base.PayloadB64)
		if err != nil {
			return FixtureRecord{}, problem.Wrap(err, problem.ValidationFailed, "invalid payload_b64")
		}
		rec.PayloadB64 = base.PayloadB64
		rec.Envelope.Payload = payloadBytes
	default:
		return FixtureRecord{}, problem.Newf(problem.ValidationFailed, "unsupported content_type %q", base.ContentType)
	}

	if p := rec.Envelope.Validate(); p != nil {
		return FixtureRecord{}, p
	}
	return rec, nil
}

func validateDecodedFixtureSubject(rec FixtureRecord) *problem.Problem {
	want := envelope.SubjectFromEnvelope(rec.Envelope)
	if want == rec.Subject {
		return nil
	}
	return problem.Newf(problem.ValidationFailed, "subject mismatch: line=%q envelope=%q", rec.Subject, want)
}

func annotateLine(p *problem.Problem, line int) *problem.Problem {
	if p == nil {
		return nil
	}
	return problem.WithDetail(p, "line", strconv.Itoa(line))
}
