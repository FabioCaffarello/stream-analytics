package jetstream

import (
	"context"
	"errors"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

func ensureStream(ctx context.Context, js nats.JetStreamContext, cfg PublisherConfig) *problem.Problem {
	streamCfg := buildStreamConfig(cfg)
	if p := validateStreamConfigInvariants(*streamCfg); p != nil {
		return p
	}

	info, err := js.StreamInfo(cfg.StreamName, nats.Context(ctx))
	switch {
	case err == nil:
		if !streamConfigMatches(info.Config, *streamCfg) {
			if _, updateErr := js.UpdateStream(streamCfg, nats.Context(ctx)); updateErr != nil {
				return wrapUnavailable("stream_update_failed", updateErr, "jetstream stream update failed")
			}
		}
		return nil
	case errors.Is(err, nats.ErrStreamNotFound):
		if _, addErr := js.AddStream(streamCfg, nats.Context(ctx)); addErr != nil {
			return wrapUnavailable("stream_create_failed", addErr, "jetstream stream create failed")
		}
		return nil
	default:
		return wrapUnavailable("stream_info_failed", err, "jetstream stream info failed")
	}
}

func buildStreamConfig(cfg PublisherConfig) *nats.StreamConfig {
	cfg = withDefaults(cfg)
	return &nats.StreamConfig{
		Name:       cfg.StreamName,
		Subjects:   append([]string(nil), subjectWildcards...),
		Retention:  nats.LimitsPolicy,
		Storage:    nats.FileStorage,
		MaxAge:     cfg.MaxAge,
		MaxBytes:   cfg.MaxBytes,
		Duplicates: cfg.DedupWindow,
	}
}

func validateStreamConfigInvariants(cfg nats.StreamConfig) *problem.Problem {
	if strings.TrimSpace(cfg.Name) == "" {
		return problem.New(problem.ValidationFailed, "jetstream stream name must not be empty")
	}
	if len(cfg.Subjects) == 0 {
		return problem.New(problem.ValidationFailed, "jetstream stream subjects must not be empty")
	}
	for i, subject := range cfg.Subjects {
		if err := ValidateSubjectPattern(subject); err != nil {
			return problem.Newf(problem.ValidationFailed, "jetstream stream subjects[%d] invalid: %v", i, err)
		}
	}
	if cfg.MaxAge <= 0 && cfg.MaxBytes <= 0 && cfg.MaxMsgs <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream stream requires at least one bound (max_age|max_bytes|max_msgs)")
	}
	if cfg.MaxAge <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream stream max_age must be > 0")
	}
	if cfg.MaxBytes < 0 || cfg.MaxMsgs < 0 {
		return problem.New(problem.ValidationFailed, "jetstream stream max_bytes/max_msgs must be >= 0")
	}
	if cfg.Duplicates <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream stream dedup_window must be > 0")
	}
	return nil
}

func streamConfigMatches(current nats.StreamConfig, desired nats.StreamConfig) bool {
	if current.Name != desired.Name {
		return false
	}
	if current.Retention != desired.Retention || current.Storage != desired.Storage {
		return false
	}
	if current.MaxAge != desired.MaxAge || current.MaxBytes != desired.MaxBytes || current.Duplicates != desired.Duplicates {
		return false
	}
	if len(current.Subjects) != len(desired.Subjects) {
		return false
	}
	for i := range desired.Subjects {
		if current.Subjects[i] != desired.Subjects[i] {
			return false
		}
	}
	return true
}
