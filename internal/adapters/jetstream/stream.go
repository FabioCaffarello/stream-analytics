package jetstream

import (
	"context"
	"errors"

	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

func ensureStream(ctx context.Context, js nats.JetStreamContext, cfg PublisherConfig) *problem.Problem {
	streamCfg := &nats.StreamConfig{
		Name:       cfg.StreamName,
		Subjects:   []string{subjectWildcard},
		Retention:  nats.LimitsPolicy,
		Storage:    nats.FileStorage,
		MaxAge:     cfg.MaxAge,
		MaxBytes:   cfg.MaxBytes,
		Duplicates: cfg.DedupWindow,
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
