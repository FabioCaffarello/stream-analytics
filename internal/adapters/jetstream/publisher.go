package jetstream

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/observability"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const (
	defaultPublishTimeout = 5 * time.Second
	defaultCloseTimeout   = 2 * time.Second
	subjectPrefixMetaKey  = "subject_prefix"
	busTypeJetStream      = "jetstream"
)

var subjectWildcards = []string{
	"dataplane.>",
	"marketdata.>",
	"aggregation.>",
	"evidence.>",
	"insights.>",
	"liquidity.>",
	"signal.>",
	"strategy.>",
	"execution.>",
	"portfolio.>",
	"quarantine.>",
}

// PublisherConfig defines JetStream publisher runtime behavior.
type PublisherConfig struct {
	URL            string
	StreamName     string
	DedupWindow    time.Duration
	MaxAge         time.Duration
	MaxBytes       int64
	PublishTimeout time.Duration
}

// Publisher implements EventPublisher over NATS JetStream.
type Publisher struct {
	nc       *nats.Conn
	js       nats.JetStreamContext
	cfg      PublisherConfig
	observer observability.BusObserver
}

// NewPublisher creates a JetStream publisher and ensures stream bootstrap.
func NewPublisher(ctx context.Context, cfg PublisherConfig, observer observability.BusObserver) (*Publisher, *problem.Problem) {
	cfg = withDefaults(cfg)
	if p := validateConfig(cfg); p != nil {
		return nil, p
	}
	if observer == nil {
		observer = observability.NopBusObserver()
	}

	nc, err := nats.Connect(
		cfg.URL,
		nats.Name("stream-analytics-jetstream-publisher"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, wrapUnavailable("connect_failed", err, "jetstream connect failed")
	}

	js, err := nc.JetStream()
	if err != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, wrapUnavailable("context_failed", err, "jetstream context create failed")
	}

	publisher := &Publisher{
		nc:       nc,
		js:       js,
		cfg:      cfg,
		observer: observer,
	}
	if p := ensureStream(ctx, js, cfg); p != nil {
		_ = publisher.Close(context.Background())
		return nil, p
	}
	return publisher, nil
}

// Publish marshals envelope and publishes it to JetStream with NATS Msg-ID.
func (p *Publisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
	subject := subjectFromEnvelopeWithPrefix(env)
	if err := ValidateSubjectTaxonomy(subject); err != nil {
		p.observer.IncPublishError("validation")
		return problem.Newf(problem.ValidationFailed, "jetstream publish subject taxonomy invalid: %v", err)
	}
	data, prob := envelope.MarshalBinary(env)
	if prob != nil {
		p.observer.IncPublishError("validation")
		return prob
	}

	msg := nats.NewMsg(subject)
	msg.Data = data
	msg.Header.Set(nats.MsgIdHdr, strings.TrimSpace(env.IdempotencyKey))

	pubCtx, cancel := context.WithTimeout(ctx, p.cfg.PublishTimeout)
	defer cancel()

	started := time.Now()
	ack, err := p.js.PublishMsg(msg, nats.Context(pubCtx))
	latency := time.Since(started)
	p.observer.ObservePublishLatency(busTypeJetStream, latency)
	if err != nil {
		kind := classifyPublishError(err)
		p.observer.IncPublishError(kind)
		if kind == "validation" {
			return problem.Wrap(err, problem.ValidationFailed, "jetstream publish validation failed")
		}
		return wrapUnavailable(kind, err, "jetstream publish failed")
	}

	if ack != nil && ack.Duplicate {
		return nil
	}
	p.observer.IncPublished(env.Type, env.Venue)
	return nil
}

// Close gracefully drains publisher connection.
func (p *Publisher) Close(ctx context.Context) *problem.Problem {
	if p == nil || p.nc == nil {
		return nil
	}

	closeCtx := ctx
	if closeCtx == nil {
		var cancel context.CancelFunc
		closeCtx, cancel = context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()
	}

	done := make(chan error, 1)
	go func() {
		done <- p.nc.Drain()
	}()

	select {
	case err := <-done:
		p.nc.Close()
		if err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
			return wrapUnavailable("drain_failed", err, "jetstream drain failed")
		}
		return nil
	case <-closeCtx.Done():
		p.nc.Close()
		return wrapUnavailable("drain_timeout", closeCtx.Err(), "jetstream drain timed out")
	}
}

func withDefaults(cfg PublisherConfig) PublisherConfig {
	if cfg.StreamName == "" {
		cfg.StreamName = "MARKETDATA"
	}
	if cfg.DedupWindow <= 0 {
		cfg.DedupWindow = 5 * time.Minute
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = 24 * time.Hour
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 10_000_000_000
	}
	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = defaultPublishTimeout
	}
	return cfg
}

func validateConfig(cfg PublisherConfig) *problem.Problem {
	if strings.TrimSpace(cfg.URL) == "" {
		return problem.New(problem.ValidationFailed, "jetstream url must not be empty")
	}
	if strings.TrimSpace(cfg.StreamName) == "" {
		return problem.New(problem.ValidationFailed, "jetstream stream_name must not be empty")
	}
	if cfg.DedupWindow <= 0 || cfg.MaxAge <= 0 || cfg.MaxBytes <= 0 || cfg.PublishTimeout <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream publisher config has non-positive values")
	}
	return nil
}

func classifyPublishError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, nats.ErrTimeout):
		return "timeout"
	case errors.Is(err, nats.ErrBadSubject):
		return "validation"
	case errors.Is(err, nats.ErrConnectionClosed), errors.Is(err, nats.ErrDisconnected), errors.Is(err, nats.ErrNoResponders):
		return "unavailable"
	default:
		return "publish_failed"
	}
}

func wrapUnavailable(kind string, err error, msg string) *problem.Problem {
	p := problem.WithDetail(problem.Wrap(err, problem.Unavailable, msg), "kind", kind)
	return problem.WithRetryable(p)
}

func (p *Publisher) String() string {
	return fmt.Sprintf("jetstream publisher(stream=%s url=%s)", p.cfg.StreamName, p.cfg.URL)
}

func subjectFromEnvelopeWithPrefix(env envelope.Envelope) string {
	base := envelope.SubjectFromEnvelope(env)
	if len(env.Meta) == 0 {
		return base
	}
	prefix := strings.TrimSpace(env.Meta[subjectPrefixMetaKey])
	if prefix == "" {
		return base
	}

	parts := strings.Split(base, ".")
	if len(parts) < 4 {
		return base
	}
	prefix = strings.Trim(strings.TrimSpace(prefix), ".")
	if prefix == "" {
		return base
	}
	suffix := strings.Join(parts[len(parts)-2:], ".")
	return prefix + "." + suffix
}
