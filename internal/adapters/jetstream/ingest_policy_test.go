package jetstream

import (
	"errors"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// parseSubjectMeta
// ---------------------------------------------------------------------------

func TestParseSubjectMeta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		subject        string
		wantType       string
		wantVersion    int
		wantVenue      string
		wantInstrument string
	}{
		{
			name:           "standard subject with v prefix",
			subject:        "marketdata.trade.v1.binance.BTC-PERP",
			wantType:       "marketdata.trade",
			wantVersion:    1,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "multi-segment event type with v prefix",
			subject:        "marketdata.bookdelta.v2.coinbase.ETH-USD",
			wantType:       "marketdata.bookdelta",
			wantVersion:    2,
			wantVenue:      "coinbase",
			wantInstrument: "ETH-USD",
		},
		{
			name:           "empty string returns zero values",
			subject:        "",
			wantType:       "",
			wantVersion:    0,
			wantVenue:      "",
			wantInstrument: "",
		},
		{
			name:           "fewer than 4 parts returns zero values",
			subject:        "trade.v1.binance",
			wantType:       "",
			wantVersion:    0,
			wantVenue:      "",
			wantInstrument: "",
		},
		{
			name:           "two parts returns zero values",
			subject:        "trade.v1",
			wantType:       "",
			wantVersion:    0,
			wantVenue:      "",
			wantInstrument: "",
		},
		{
			name:           "exactly 4 parts with v prefix",
			subject:        "trade.v1.binance.BTC-PERP",
			wantType:       "trade",
			wantVersion:    1,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "missing v prefix falls back to venue and instrument only",
			subject:        "trade.42.binance.BTC-PERP",
			wantType:       "",
			wantVersion:    0,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "v prefix with zero version yields version 0",
			subject:        "marketdata.trade.v0.binance.BTC-PERP",
			wantType:       "marketdata.trade",
			wantVersion:    0,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "v prefix with negative number yields version 0",
			subject:        "trade.v-1.binance.BTC-PERP",
			wantType:       "trade",
			wantVersion:    0,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "v prefix with non-numeric suffix yields version 0",
			subject:        "trade.vabc.binance.BTC-PERP",
			wantType:       "trade",
			wantVersion:    0,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "uppercase V prefix is recognized",
			subject:        "trade.V3.binance.BTC-PERP",
			wantType:       "trade",
			wantVersion:    3,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "whitespace subject is trimmed",
			subject:        "  marketdata.trade.v1.binance.BTC-PERP  ",
			wantType:       "marketdata.trade",
			wantVersion:    1,
			wantVenue:      "binance",
			wantInstrument: "BTC-PERP",
		},
		{
			name:           "five-segment type plus version venue instrument",
			subject:        "a.b.c.v5.kraken.SOL-PERP",
			wantType:       "a.b.c",
			wantVersion:    5,
			wantVenue:      "kraken",
			wantInstrument: "SOL-PERP",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotType, gotVer, gotVenue, gotInstr := parseSubjectMeta(tc.subject)
			if gotType != tc.wantType {
				t.Errorf("eventType=%q want=%q", gotType, tc.wantType)
			}
			if gotVer != tc.wantVersion {
				t.Errorf("version=%d want=%d", gotVer, tc.wantVersion)
			}
			if gotVenue != tc.wantVenue {
				t.Errorf("venue=%q want=%q", gotVenue, tc.wantVenue)
			}
			if gotInstr != tc.wantInstrument {
				t.Errorf("instrument=%q want=%q", gotInstr, tc.wantInstrument)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// reasonCodeFromProblem
// ---------------------------------------------------------------------------

func TestReasonCodeFromProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    *problem.Problem
		want string
	}{
		{
			name: "nil problem returns empty",
			p:    nil,
			want: "",
		},
		{
			name: "Details with reason_code string is returned directly",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason_code", ingestReasonDecodeFailed),
			want: ingestReasonDecodeFailed,
		},
		{
			name: "Details reason_code non-string is ignored",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason_code", 42),
			want: "",
		},
		{
			name: "Details reason with unknown_event_type substring",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason", "unknown_event_type"),
			want: ingestReasonUnknownEventType,
		},
		{
			name: "Details reason with missing_payload_codec substring",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason", "missing_payload_codec for version"),
			want: ingestReasonUnknownEventVersion,
		},
		{
			name: "Details reason with json_fallback_decode substring",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason", "json_fallback_decode failed"),
			want: ingestReasonDecodeFailed,
		},
		{
			name: "Details reason with unknown_content_type substring",
			p:    problem.WithDetail(problem.New(problem.Internal, "something"), "reason", "unknown_content_type: cbor"),
			want: ingestReasonValidationFailed,
		},
		{
			name: "message with unhandled envelope type",
			p:    problem.New(problem.Internal, "unhandled envelope type: foo.bar"),
			want: ingestReasonUnknownEventType,
		},
		{
			name: "message with unsupported envelope version",
			p:    problem.New(problem.Internal, "unsupported envelope version 99"),
			want: ingestReasonUnknownEventVersion,
		},
		{
			name: "message containing decode keyword",
			p:    problem.New(problem.Internal, "failed to decode payload"),
			want: ingestReasonDecodeFailed,
		},
		{
			name: "message containing unmarshal keyword",
			p:    problem.New(problem.Internal, "JSON unmarshal error"),
			want: ingestReasonDecodeFailed,
		},
		{
			name: "message with no matching keywords returns empty",
			p:    problem.New(problem.Internal, "something completely different"),
			want: "",
		},
		{
			name: "empty Details and empty message returns empty",
			p:    problem.New(problem.Internal, ""),
			want: "",
		},
		{
			name: "reason_code takes priority over reason field",
			p: func() *problem.Problem {
				p := problem.WithDetail(problem.New(problem.Internal, "something"), "reason_code", ingestReasonUnknownEventType)
				return problem.WithDetail(p, "reason", "json_fallback_decode")
			}(),
			want: ingestReasonUnknownEventType,
		},
		{
			name: "message matching is case insensitive",
			p:    problem.New(problem.Internal, "UNHANDLED ENVELOPE TYPE"),
			want: ingestReasonUnknownEventType,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := reasonCodeFromProblem(tc.p)
			if got != tc.want {
				t.Errorf("reasonCodeFromProblem()=%q want=%q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isKnownEventVersionUnsupported
// ---------------------------------------------------------------------------

func TestIsKnownEventVersionUnsupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  envelope.Envelope
		want bool
	}{
		{
			name: "marketdata.trade version 1 is supported",
			env:  envelope.Envelope{Type: "marketdata.trade", Version: 1},
			want: false,
		},
		{
			name: "marketdata.trade version 2 is unsupported",
			env:  envelope.Envelope{Type: "marketdata.trade", Version: 2},
			want: true,
		},
		{
			name: "marketdata.trade version 99 is unsupported",
			env:  envelope.Envelope{Type: "marketdata.trade", Version: 99},
			want: true,
		},
		{
			name: "marketdata.trade version 0 is not unsupported (not > 0)",
			env:  envelope.Envelope{Type: "marketdata.trade", Version: 0},
			want: false,
		},
		{
			name: "marketdata.bookdelta version 1 is supported",
			env:  envelope.Envelope{Type: "marketdata.bookdelta", Version: 1},
			want: false,
		},
		{
			name: "marketdata.bookdelta version 3 is unsupported",
			env:  envelope.Envelope{Type: "marketdata.bookdelta", Version: 3},
			want: true,
		},
		{
			name: "marketdata.raw version 1 is supported",
			env:  envelope.Envelope{Type: "marketdata.raw", Version: 1},
			want: false,
		},
		{
			name: "marketdata.raw version 5 is unsupported",
			env:  envelope.Envelope{Type: "marketdata.raw", Version: 5},
			want: true,
		},
		{
			name: "unknown event type is never unsupported",
			env:  envelope.Envelope{Type: "something.else", Version: 99},
			want: false,
		},
		{
			name: "empty event type is never unsupported",
			env:  envelope.Envelope{Type: "", Version: 99},
			want: false,
		},
		{
			name: "case insensitive matching for known type",
			env:  envelope.Envelope{Type: "Marketdata.Trade", Version: 2},
			want: true,
		},
		{
			name: "known type with whitespace is matched",
			env:  envelope.Envelope{Type: "  marketdata.trade  ", Version: 2},
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isKnownEventVersionUnsupported(tc.env)
			if got != tc.want {
				t.Errorf("isKnownEventVersionUnsupported()=%v want=%v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeIngestReason
// ---------------------------------------------------------------------------

func TestNormalizeIngestReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reasonCode string
		want       string
	}{
		{
			name:       "OK is passed through",
			reasonCode: "OK",
			want:       "OK",
		},
		{
			name:       "DECODE_FAILED is passed through",
			reasonCode: "DECODE_FAILED",
			want:       "DECODE_FAILED",
		},
		{
			name:       "VALIDATION_FAILED is passed through",
			reasonCode: "VALIDATION_FAILED",
			want:       "VALIDATION_FAILED",
		},
		{
			name:       "UNKNOWN_EVENT_TYPE is passed through",
			reasonCode: "UNKNOWN_EVENT_TYPE",
			want:       "UNKNOWN_EVENT_TYPE",
		},
		{
			name:       "UNKNOWN_EVENT_VERSION is passed through",
			reasonCode: "UNKNOWN_EVENT_VERSION",
			want:       "UNKNOWN_EVENT_VERSION",
		},
		{
			name:       "TRANSIENT_FAILURE is passed through",
			reasonCode: "TRANSIENT_FAILURE",
			want:       "TRANSIENT_FAILURE",
		},
		{
			name:       "TRANSIENT_EXHAUSTED is passed through",
			reasonCode: "TRANSIENT_EXHAUSTED",
			want:       "TRANSIENT_EXHAUSTED",
		},
		{
			name:       "QUARANTINE_PUBLISH_FAILED is passed through",
			reasonCode: "QUARANTINE_PUBLISH_FAILED",
			want:       "QUARANTINE_PUBLISH_FAILED",
		},
		{
			name:       "lowercase input is uppercased",
			reasonCode: "decode_failed",
			want:       "DECODE_FAILED",
		},
		{
			name:       "mixed case input is uppercased",
			reasonCode: "Transient_Failure",
			want:       "TRANSIENT_FAILURE",
		},
		{
			name:       "unknown reason returns empty string",
			reasonCode: "SOMETHING_RANDOM",
			want:       "",
		},
		{
			name:       "empty string returns empty string",
			reasonCode: "",
			want:       "",
		},
		{
			name:       "whitespace-only returns empty string",
			reasonCode: "   ",
			want:       "",
		},
		{
			name:       "known reason with surrounding whitespace is trimmed",
			reasonCode: "  OK  ",
			want:       "OK",
		},
		{
			name:       "BACKPRESSURE_DROP is not in known set returns empty",
			reasonCode: "BACKPRESSURE_DROP",
			want:       "",
		},
		{
			name:       "BUFFER_FULL_DROP is not in known set returns empty",
			reasonCode: "BUFFER_FULL_DROP",
			want:       "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeIngestReason(tc.reasonCode)
			if got != tc.want {
				t.Errorf("normalizeIngestReason(%q)=%q want=%q", tc.reasonCode, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeBoundedProblemText
// ---------------------------------------------------------------------------

func TestNormalizeBoundedProblemText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		p      *problem.Problem
		maxLen int
		want   string
	}{
		{
			name:   "nil problem returns empty string",
			p:      nil,
			maxLen: 100,
			want:   "",
		},
		{
			name:   "short message under maxLen is returned as-is",
			p:      problem.New(problem.Internal, "short error"),
			maxLen: 100,
			want:   "short error",
		},
		{
			name:   "message exactly at maxLen boundary is not truncated",
			p:      problem.New(problem.Internal, "12345"),
			maxLen: 5,
			want:   "12345",
		},
		{
			name:   "message exceeding maxLen is truncated",
			p:      problem.New(problem.Internal, "this is a very long message that exceeds limit"),
			maxLen: 10,
			want:   "this is a ",
		},
		{
			name:   "zero maxLen returns full message",
			p:      problem.New(problem.Internal, "hello world"),
			maxLen: 0,
			want:   "hello world",
		},
		{
			name:   "negative maxLen returns full message",
			p:      problem.New(problem.Internal, "hello world"),
			maxLen: -1,
			want:   "hello world",
		},
		{
			name:   "whitespace is collapsed and trimmed",
			p:      problem.New(problem.Internal, "  hello   world  "),
			maxLen: 100,
			want:   "hello world",
		},
		{
			name:   "empty message returns empty string",
			p:      problem.New(problem.Internal, ""),
			maxLen: 100,
			want:   "",
		},
		{
			name:   "maxLen of 1 truncates to single character",
			p:      problem.New(problem.Internal, "abcdef"),
			maxLen: 1,
			want:   "a",
		},
		{
			name:   "whitespace collapsing then truncation",
			p:      problem.New(problem.Internal, "  a   b   c  "),
			maxLen: 3,
			want:   "a b",
		},
		{
			name:   "quarantineErrorMaxLen constant is 512",
			p:      problem.New(problem.Internal, strings.Repeat("x", 600)),
			maxLen: quarantineErrorMaxLen,
			want:   strings.Repeat("x", 512),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeBoundedProblemText(tc.p, tc.maxLen)
			if got != tc.want {
				t.Errorf("normalizeBoundedProblemText()=%q want=%q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// looksLikeDecodeFailure
// ---------------------------------------------------------------------------

func TestLooksLikeDecodeFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		p    *problem.Problem
		want bool
	}{
		{
			name: "nil problem returns false",
			p:    nil,
			want: false,
		},
		{
			name: "message with decode keyword returns true",
			p:    problem.New(problem.Internal, "failed to decode payload"),
			want: true,
		},
		{
			name: "message with unmarshal keyword returns true",
			p:    problem.New(problem.Internal, "JSON unmarshal error"),
			want: true,
		},
		{
			name: "message with uppercase DECODE is detected (case insensitive)",
			p:    problem.New(problem.Internal, "DECODE failure"),
			want: true,
		},
		{
			name: "message without decode-related keywords returns false",
			p:    problem.New(problem.Internal, "connection timeout"),
			want: false,
		},
		{
			name: "empty message returns false",
			p:    problem.New(problem.Internal, ""),
			want: false,
		},
		{
			name: "cause with json keyword returns true",
			p:    problem.Wrap(errors.New("invalid json input"), problem.Internal, "processing failed"),
			want: true,
		},
		{
			name: "cause with unmarshal keyword returns true",
			p:    problem.Wrap(errors.New("unmarshal: unexpected token"), problem.Internal, "processing failed"),
			want: true,
		},
		{
			name: "cause without decode keywords and message without decode keywords returns false",
			p:    problem.Wrap(errors.New("network reset"), problem.Internal, "processing failed"),
			want: false,
		},
		{
			name: "message matches even when cause does not",
			p:    problem.Wrap(errors.New("something else"), problem.Internal, "decode error occurred"),
			want: true,
		},
		{
			name: "cause matches even when message does not",
			p:    problem.Wrap(errors.New("json parse error"), problem.Internal, "operation failed"),
			want: true,
		},
		{
			name: "whitespace in message is handled",
			p:    problem.New(problem.Internal, "  decode  "),
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := looksLikeDecodeFailure(tc.p)
			if got != tc.want {
				t.Errorf("looksLikeDecodeFailure()=%v want=%v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyEnvelopeDecodeFailure
// ---------------------------------------------------------------------------

func TestClassifyEnvelopeDecodeFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		p              *problem.Problem
		wantReason     string
		wantDisp       Disposition
		wantQuarantine bool
	}{
		{
			name:           "nil problem returns DECODE_FAILED poison",
			p:              nil,
			wantReason:     ingestReasonDecodeFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
		{
			name:           "problem with decode message returns DECODE_FAILED poison",
			p:              problem.New(problem.Internal, "failed to decode envelope"),
			wantReason:     ingestReasonDecodeFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
		{
			name:           "problem with unmarshal message returns DECODE_FAILED poison",
			p:              problem.New(problem.Internal, "unmarshal envelope failed"),
			wantReason:     ingestReasonDecodeFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
		{
			name:           "problem with json cause returns DECODE_FAILED poison",
			p:              problem.Wrap(errors.New("invalid json"), problem.Internal, "envelope processing error"),
			wantReason:     ingestReasonDecodeFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
		{
			name:           "non-decode problem returns VALIDATION_FAILED poison",
			p:              problem.New(problem.ValidationFailed, "missing required field"),
			wantReason:     ingestReasonValidationFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
		{
			name:           "internal error without decode keywords returns VALIDATION_FAILED",
			p:              problem.New(problem.Internal, "something went wrong"),
			wantReason:     ingestReasonValidationFailed,
			wantDisp:       DispositionTerm,
			wantQuarantine: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := classifyEnvelopeDecodeFailure(tc.p)
			if got.ReasonCode != tc.wantReason {
				t.Errorf("reasonCode=%q want=%q", got.ReasonCode, tc.wantReason)
			}
			if got.Disposition != tc.wantDisp {
				t.Errorf("disposition=%v want=%v", got.Disposition, tc.wantDisp)
			}
			if got.Quarantine != tc.wantQuarantine {
				t.Errorf("quarantine=%v want=%v", got.Quarantine, tc.wantQuarantine)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// fallbackOrDefault
// ---------------------------------------------------------------------------

func TestFallbackOrDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{
			name:     "non-empty value is returned",
			value:    "primary",
			fallback: "secondary",
			want:     "primary",
		},
		{
			name:     "empty value returns fallback",
			value:    "",
			fallback: "secondary",
			want:     "secondary",
		},
		{
			name:     "whitespace-only value returns fallback",
			value:    "   ",
			fallback: "secondary",
			want:     "secondary",
		},
		{
			name:     "both empty returns empty",
			value:    "",
			fallback: "",
			want:     "",
		},
		{
			name:     "value with whitespace is trimmed",
			value:    "  hello  ",
			fallback: "world",
			want:     "hello",
		},
		{
			name:     "fallback with whitespace is trimmed",
			value:    "",
			fallback: "  world  ",
			want:     "world",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fallbackOrDefault(tc.value, tc.fallback)
			if got != tc.want {
				t.Errorf("fallbackOrDefault(%q, %q)=%q want=%q", tc.value, tc.fallback, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// fallbackVersion
// ---------------------------------------------------------------------------

func TestFallbackVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  int
		fallback int
		want     int
	}{
		{
			name:     "positive version is returned",
			version:  3,
			fallback: 5,
			want:     3,
		},
		{
			name:     "zero version uses positive fallback",
			version:  0,
			fallback: 2,
			want:     2,
		},
		{
			name:     "negative version uses positive fallback",
			version:  -1,
			fallback: 7,
			want:     7,
		},
		{
			name:     "both zero returns default 1",
			version:  0,
			fallback: 0,
			want:     1,
		},
		{
			name:     "both negative returns default 1",
			version:  -1,
			fallback: -1,
			want:     1,
		},
		{
			name:     "zero version and negative fallback returns default 1",
			version:  0,
			fallback: -1,
			want:     1,
		},
		{
			name:     "version 1 is returned directly",
			version:  1,
			fallback: 0,
			want:     1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := fallbackVersion(tc.version, tc.fallback)
			if got != tc.want {
				t.Errorf("fallbackVersion(%d, %d)=%d want=%d", tc.version, tc.fallback, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeOrDefaultContentType
// ---------------------------------------------------------------------------

func TestNormalizeOrDefaultContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		want        string
	}{
		{
			name:        "empty string defaults to application/json",
			contentType: "",
			want:        envelope.ContentTypeJSON,
		},
		{
			name:        "whitespace-only defaults to application/json",
			contentType: "   ",
			want:        envelope.ContentTypeJSON,
		},
		{
			name:        "application/json is preserved",
			contentType: "application/json",
			want:        envelope.ContentTypeJSON,
		},
		{
			name:        "application/protobuf is preserved",
			contentType: "application/protobuf",
			want:        envelope.ContentTypeProto,
		},
		{
			name:        "unknown content type defaults to application/json",
			contentType: "text/xml",
			want:        envelope.ContentTypeJSON,
		},
		{
			name:        "content type with whitespace is trimmed before check",
			contentType: "  application/json  ",
			want:        envelope.ContentTypeJSON,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeOrDefaultContentType(tc.contentType)
			if got != tc.want {
				t.Errorf("normalizeOrDefaultContentType(%q)=%q want=%q", tc.contentType, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// boundedPositiveTsIngest
// ---------------------------------------------------------------------------

func TestBoundedPositiveTsIngest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tsIngest int64
		want     int64
	}{
		{name: "positive value is returned", tsIngest: 1000, want: 1000},
		{name: "zero clamps to 1", tsIngest: 0, want: 1},
		{name: "negative clamps to 1", tsIngest: -5, want: 1},
		{name: "value 1 is returned", tsIngest: 1, want: 1},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := boundedPositiveTsIngest(tc.tsIngest); got != tc.want {
				t.Errorf("boundedPositiveTsIngest(%d)=%d want=%d", tc.tsIngest, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// boundedNonNegativeSeq
// ---------------------------------------------------------------------------

func TestBoundedNonNegativeSeq(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		seq  int64
		want int64
	}{
		{name: "positive value is returned", seq: 42, want: 42},
		{name: "zero is returned", seq: 0, want: 0},
		{name: "negative clamps to 0", seq: -1, want: 0},
		{name: "large negative clamps to 0", seq: -9999, want: 0},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := boundedNonNegativeSeq(tc.seq); got != tc.want {
				t.Errorf("boundedNonNegativeSeq(%d)=%d want=%d", tc.seq, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ClassifyIngestError (additional edge cases beyond conformance golden table)
// ---------------------------------------------------------------------------

func TestClassifyIngestError_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		p          *problem.Problem
		env        envelope.Envelope
		wantDisp   Disposition
		wantReason string
	}{
		{
			name:       "nil problem returns OK ack",
			p:          nil,
			env:        envelope.Envelope{},
			wantDisp:   DispositionAck,
			wantReason: ingestReasonOK,
		},
		{
			name: "retryable problem returns NAK transient",
			p: problem.WithRetryable(
				problem.New(problem.Internal, "temporary issue"),
			),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionNak,
			wantReason: ingestReasonTransientFailure,
		},
		{
			name:       "Unavailable code returns NAK transient",
			p:          problem.New(problem.Unavailable, "service unavailable"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionNak,
			wantReason: ingestReasonTransientFailure,
		},
		{
			name:       "Internal code without decode keywords returns NAK transient",
			p:          problem.New(problem.Internal, "unexpected error"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionNak,
			wantReason: ingestReasonTransientFailure,
		},
		{
			name:       "Internal code with decode keyword is NOT transient (caught by looksLikeDecodeFailure)",
			p:          problem.New(problem.Internal, "failed to decode message"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonDecodeFailed,
		},
		{
			name:       "InvalidArgument code returns TERM validation_failed",
			p:          problem.New(problem.InvalidArgument, "bad argument"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "NotFound code returns TERM validation_failed",
			p:          problem.New(problem.NotFound, "resource not found"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "Conflict code returns TERM validation_failed",
			p:          problem.New(problem.Conflict, "conflict"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "OutOfOrder code returns TERM validation_failed",
			p:          problem.New(problem.OutOfOrder, "out of order"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "Duplicate code returns TERM validation_failed",
			p:          problem.New(problem.Duplicate, "duplicate"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "IntegrityViolation code returns TERM validation_failed",
			p:          problem.New(problem.IntegrityViolation, "integrity broken"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 1},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonValidationFailed,
		},
		{
			name:       "known type with unsupported version overrides generic error",
			p:          problem.New(problem.Internal, "some generic error"),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 99},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonUnknownEventVersion,
		},
		{
			name: "reason_code in details takes priority over version check",
			p: problem.WithDetail(
				problem.New(problem.Internal, "some error"),
				"reason_code", ingestReasonDecodeFailed,
			),
			env:        envelope.Envelope{Type: "marketdata.trade", Version: 99},
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonDecodeFailed,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyIngestError(tc.p, tc.env)
			if got.Disposition != tc.wantDisp {
				t.Errorf("disposition=%v want=%v", got.Disposition, tc.wantDisp)
			}
			if got.ReasonCode != tc.wantReason {
				t.Errorf("reasonCode=%q want=%q", got.ReasonCode, tc.wantReason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// applyQuarantinePublishResult
// ---------------------------------------------------------------------------

func TestApplyQuarantinePublishResult(t *testing.T) {
	t.Parallel()

	base := poisonDecision(ingestReasonDecodeFailed)

	tests := []struct {
		name       string
		decision   ingestDecision
		err        *problem.Problem
		wantDisp   Disposition
		wantReason string
	}{
		{
			name:       "nil quarantine error preserves original decision",
			decision:   base,
			err:        nil,
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonDecodeFailed,
		},
		{
			name:       "non-retryable quarantine error returns TERM",
			decision:   base,
			err:        problem.New(problem.Unavailable, "quarantine publish failed"),
			wantDisp:   DispositionTerm,
			wantReason: ingestReasonQuarantinePublishError,
		},
		{
			name:       "retryable quarantine error returns NAK",
			decision:   base,
			err:        problem.WithRetryable(problem.New(problem.Unavailable, "quarantine timeout")),
			wantDisp:   DispositionNak,
			wantReason: ingestReasonQuarantinePublishError,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := applyQuarantinePublishResult(tc.decision, tc.err)
			if got.Disposition != tc.wantDisp {
				t.Errorf("disposition=%v want=%v", got.Disposition, tc.wantDisp)
			}
			if got.ReasonCode != tc.wantReason {
				t.Errorf("reasonCode=%q want=%q", got.ReasonCode, tc.wantReason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// poisonDecision and transientDecision (structural invariants)
// ---------------------------------------------------------------------------

func TestPoisonDecision_Invariants(t *testing.T) {
	t.Parallel()

	d := poisonDecision(ingestReasonUnknownEventType)
	if d.Disposition != DispositionTerm {
		t.Errorf("disposition=%v want=%v", d.Disposition, DispositionTerm)
	}
	if d.Status != "term" {
		t.Errorf("status=%q want=%q", d.Status, "term")
	}
	if !d.Quarantine {
		t.Error("quarantine=false want=true")
	}
	if d.Drop {
		t.Error("drop=true want=false")
	}
}

func TestTransientDecision_Invariants(t *testing.T) {
	t.Parallel()

	d := transientDecision(ingestReasonTransientFailure)
	if d.Disposition != DispositionNak {
		t.Errorf("disposition=%v want=%v", d.Disposition, DispositionNak)
	}
	if d.Status != "nak" {
		t.Errorf("status=%q want=%q", d.Status, "nak")
	}
	if d.Quarantine {
		t.Error("quarantine=true want=false")
	}
	if d.Drop {
		t.Error("drop=true want=false")
	}
}
