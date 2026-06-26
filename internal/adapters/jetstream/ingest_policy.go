package jetstream

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const (
	ingestReasonOK                     = "OK"
	ingestReasonDecodeFailed           = "DECODE_FAILED"
	ingestReasonValidationFailed       = "VALIDATION_FAILED"
	ingestReasonUnknownEventType       = "UNKNOWN_EVENT_TYPE"
	ingestReasonUnknownEventVersion    = "UNKNOWN_EVENT_VERSION"
	ingestReasonTransientFailure       = "TRANSIENT_FAILURE"
	ingestReasonTransientExhausted     = "TRANSIENT_EXHAUSTED"
	ingestReasonQuarantinePublishError = "QUARANTINE_PUBLISH_FAILED"
	ingestReasonBackpressureDrop       = "BACKPRESSURE_DROP"
	ingestReasonBufferFullDrop         = "BUFFER_FULL_DROP"

	quarantineEventType    = "quarantine"
	quarantineEventVersion = 1
	quarantineErrorMaxLen  = 512
)

type ingestDecision struct {
	Disposition Disposition
	Status      string
	ReasonCode  string
	Quarantine  bool
	Drop        bool
}

type quarantinePayload struct {
	ReasonCode string           `json:"reason_code"`
	Error      string           `json:"error"`
	Source     quarantineSource `json:"source"`
}

type quarantineSource struct {
	Subject        string `json:"subject"`
	MsgID          string `json:"msg_id"`
	EventType      string `json:"event_type"`
	Version        int    `json:"version"`
	Venue          string `json:"venue"`
	Instrument     string `json:"instrument"`
	ContentType    string `json:"content_type"`
	IdempotencyKey string `json:"idempotency_key"`
	PayloadSHA256  string `json:"payload_sha256"`
}

func ClassifyIngestError(p *problem.Problem, env envelope.Envelope) ingestDecision {
	if p == nil {
		return ingestDecision{
			Disposition: DispositionAck,
			Status:      "ok",
			ReasonCode:  ingestReasonOK,
		}
	}

	switch normalizeIngestReason(reasonCodeFromProblem(p)) {
	case ingestReasonUnknownEventType:
		return poisonDecision(ingestReasonUnknownEventType)
	case ingestReasonUnknownEventVersion:
		return poisonDecision(ingestReasonUnknownEventVersion)
	case ingestReasonDecodeFailed:
		return poisonDecision(ingestReasonDecodeFailed)
	case ingestReasonValidationFailed:
		return poisonDecision(ingestReasonValidationFailed)
	}

	if isKnownEventVersionUnsupported(env) {
		return poisonDecision(ingestReasonUnknownEventVersion)
	}

	if p.Retryable || p.Code == problem.Unavailable {
		return transientDecision(ingestReasonTransientFailure)
	}
	if p.Code == problem.Internal && !looksLikeDecodeFailure(p) {
		return transientDecision(ingestReasonTransientFailure)
	}

	switch p.Code {
	case problem.ValidationFailed,
		problem.InvalidArgument,
		problem.NotFound,
		problem.Conflict,
		problem.OutOfOrder,
		problem.Duplicate,
		problem.IntegrityViolation:
		return poisonDecision(ingestReasonValidationFailed)
	default:
		return transientDecision(ingestReasonTransientFailure)
	}
}

func classifyEnvelopeDecodeFailure(p *problem.Problem) ingestDecision {
	if p == nil {
		return poisonDecision(ingestReasonDecodeFailed)
	}
	if looksLikeDecodeFailure(p) {
		return poisonDecision(ingestReasonDecodeFailed)
	}
	return poisonDecision(ingestReasonValidationFailed)
}

func applyQuarantinePublishResult(decision ingestDecision, quarantineErr *problem.Problem) ingestDecision {
	if quarantineErr == nil {
		return decision
	}
	if !quarantineErr.Retryable {
		return ingestDecision{
			Disposition: DispositionTerm,
			Status:      "term",
			ReasonCode:  ingestReasonQuarantinePublishError,
		}
	}
	return ingestDecision{
		Disposition: DispositionNak,
		Status:      "nak",
		ReasonCode:  ingestReasonQuarantinePublishError,
	}
}

func poisonDecision(reasonCode string) ingestDecision {
	return ingestDecision{
		Disposition: DispositionTerm,
		Status:      "term",
		ReasonCode:  reasonCode,
		Quarantine:  true,
	}
}

func transientDecision(reasonCode string) ingestDecision {
	return ingestDecision{
		Disposition: DispositionNak,
		Status:      "nak",
		ReasonCode:  reasonCode,
	}
}

func reasonCodeFromProblem(p *problem.Problem) string {
	if p == nil {
		return ""
	}
	if p.Details != nil {
		if v, ok := p.Details["reason_code"]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		if v, ok := p.Details["reason"]; ok {
			if s, ok := v.(string); ok {
				switch {
				case strings.Contains(s, "unknown_event_type"):
					return ingestReasonUnknownEventType
				case strings.Contains(s, "missing_payload_codec"):
					return ingestReasonUnknownEventVersion
				case strings.Contains(s, "json_fallback_decode"):
					return ingestReasonDecodeFailed
				case strings.Contains(s, "unknown_content_type"):
					return ingestReasonValidationFailed
				}
			}
		}
	}

	msg := strings.ToLower(strings.TrimSpace(p.Message))
	switch {
	case strings.Contains(msg, "unhandled envelope type"):
		return ingestReasonUnknownEventType
	case strings.Contains(msg, "unsupported envelope version"):
		return ingestReasonUnknownEventVersion
	case strings.Contains(msg, "decode"), strings.Contains(msg, "unmarshal"):
		return ingestReasonDecodeFailed
	}
	return ""
}

func looksLikeDecodeFailure(p *problem.Problem) bool {
	if p == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(p.Message))
	if strings.Contains(msg, "decode") || strings.Contains(msg, "unmarshal") {
		return true
	}
	if p.Cause != nil {
		cause := strings.ToLower(strings.TrimSpace(p.Cause.Error()))
		if strings.Contains(cause, "json") || strings.Contains(cause, "unmarshal") {
			return true
		}
	}
	return false
}

func isKnownEventVersionUnsupported(env envelope.Envelope) bool {
	switch strings.ToLower(strings.TrimSpace(env.Type)) {
	case "marketdata.bookdelta", "marketdata.trade", "marketdata.raw":
		return env.Version > 0 && env.Version != 1
	default:
		return false
	}
}

func normalizeIngestReason(reasonCode string) string {
	switch strings.ToUpper(strings.TrimSpace(reasonCode)) {
	case ingestReasonOK,
		ingestReasonDecodeFailed,
		ingestReasonValidationFailed,
		ingestReasonUnknownEventType,
		ingestReasonUnknownEventVersion,
		ingestReasonTransientFailure,
		ingestReasonTransientExhausted,
		ingestReasonQuarantinePublishError:
		return strings.ToUpper(strings.TrimSpace(reasonCode))
	default:
		return ""
	}
}

func isQuarantineMessage(msg *nats.Msg, env envelope.Envelope) bool {
	if strings.EqualFold(strings.TrimSpace(env.Type), quarantineEventType) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.Subject)), "quarantine.v1.")
}

// ClassifyQuarantinePublishError decides whether quarantine publish failure is
// transient (NAK) or permanent (TERM).
func ClassifyQuarantinePublishError(err error) (retryable bool, reasonCode string) {
	if err == nil {
		return false, ""
	}

	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if errors.Is(err, nats.ErrAuthorization) ||
		strings.Contains(msg, "authorization violation") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "no permissions") ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, " 403") ||
		strings.Contains(msg, "403 ") {
		return false, ingestReasonQuarantinePublishError
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, nats.ErrTimeout),
		errors.Is(err, nats.ErrNoResponders),
		errors.Is(err, nats.ErrConnectionClosed),
		errors.Is(err, nats.ErrDisconnected):
		return true, ingestReasonQuarantinePublishError
	default:
		// Keep default as retryable for safety; permanent classes are explicit.
		return true, ingestReasonQuarantinePublishError
	}
}

func buildQuarantineEnvelope(msg *nats.Msg, env envelope.Envelope, reasonCode string, p *problem.Problem) (envelope.Envelope, *problem.Problem) {
	source := resolveQuarantineSource(msg, env)

	payload := quarantinePayload{
		ReasonCode: normalizeIngestReason(reasonCode),
		Error:      normalizeBoundedProblemText(p, quarantineErrorMaxLen),
		Source:     source,
	}
	if payload.ReasonCode == "" {
		payload.ReasonCode = ingestReasonValidationFailed
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return envelope.Envelope{}, problem.Wrap(err, problem.Internal, "marshal quarantine payload failed")
	}

	out := envelope.Envelope{
		Type:           quarantineEventType,
		Version:        quarantineEventVersion,
		Venue:          source.Venue,
		Instrument:     source.Instrument,
		TsExchange:     env.TsExchange,
		TsIngest:       boundedPositiveTsIngest(env.TsIngest),
		Seq:            boundedNonNegativeSeq(env.Seq),
		IdempotencyKey: sharedhash.HashFieldsFast("quarantine", payload.ReasonCode, payload.Source.Subject, payload.Source.MsgID, payload.Source.IdempotencyKey, payload.Source.PayloadSHA256),
		ContentType:    envelope.ContentTypeJSON,
		Payload:        payloadBytes,
	}
	if pp := out.Validate(); pp != nil {
		return envelope.Envelope{}, pp
	}
	if err := ValidateSubjectTaxonomy(envelope.SubjectFromEnvelope(out)); err != nil {
		return envelope.Envelope{}, problem.Newf(problem.ValidationFailed, "jetstream quarantine subject taxonomy invalid: %v", err)
	}
	return out, nil
}

func resolveQuarantineSource(msg *nats.Msg, env envelope.Envelope) quarantineSource {
	parsedType, parsedVersion, parsedVenue, parsedInstrument := parseSubjectMeta(msg.Subject)
	msgID := strings.TrimSpace(msg.Header.Get(nats.MsgIdHdr))
	payloadSHA := sharedhash.HashBytes(msg.Data)

	originalID := fallbackOrDefault(strings.TrimSpace(env.IdempotencyKey), msgID)
	if originalID == "" {
		originalID = sharedhash.HashFieldsFast(msg.Subject, payloadSHA)
	}

	return quarantineSource{
		Subject:        strings.TrimSpace(msg.Subject),
		MsgID:          msgID,
		EventType:      fallbackOrDefault(strings.TrimSpace(env.Type), parsedType),
		Version:        fallbackVersion(env.Version, parsedVersion),
		Venue:          fallbackOrDefault(strings.TrimSpace(env.Venue), fallbackOrDefault(parsedVenue, "UNKNOWN")),
		Instrument:     fallbackOrDefault(strings.TrimSpace(env.Instrument), fallbackOrDefault(parsedInstrument, "UNKNOWN")),
		ContentType:    normalizeOrDefaultContentType(env.ContentType),
		IdempotencyKey: originalID,
		PayloadSHA256:  payloadSHA,
	}
}

func fallbackOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fallback)
}

func fallbackVersion(version, fallback int) int {
	if version > 0 {
		return version
	}
	if fallback > 0 {
		return fallback
	}
	return 1
}

func normalizeOrDefaultContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return envelope.ContentTypeJSON
	}
	if normalized, p := envelope.NormalizeContentType(contentType); p == nil {
		return normalized
	}
	return envelope.ContentTypeJSON
}

func boundedPositiveTsIngest(tsIngest int64) int64 {
	if tsIngest > 0 {
		return tsIngest
	}
	return 1
}

func boundedNonNegativeSeq(seq int64) int64 {
	if seq >= 0 {
		return seq
	}
	return 0
}

func parseSubjectMeta(subject string) (eventType string, version int, venue string, instrument string) {
	parts := strings.Split(strings.TrimSpace(subject), ".")
	if len(parts) < 4 {
		return "", 0, "", ""
	}
	versionPart := strings.TrimSpace(parts[len(parts)-3])
	if !strings.HasPrefix(strings.ToLower(versionPart), "v") {
		return "", 0, strings.TrimSpace(parts[len(parts)-2]), strings.TrimSpace(parts[len(parts)-1])
	}
	v, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(versionPart), "v"))
	if err != nil || v <= 0 {
		v = 0
	}
	return strings.Join(parts[:len(parts)-3], "."), v, strings.TrimSpace(parts[len(parts)-2]), strings.TrimSpace(parts[len(parts)-1])
}

func normalizeBoundedProblemText(p *problem.Problem, maxLen int) string {
	if p == nil {
		return ""
	}
	text := strings.TrimSpace(p.Message)
	text = strings.Join(strings.Fields(text), " ")
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	return text[:maxLen]
}
