package jetstream

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const (
	defaultFetchTimeout    = 750 * time.Millisecond
	defaultLagPollInterval = 5 * time.Second
	minHeartbeatInterval   = 250 * time.Millisecond
	maxHeartbeatInterval   = 5 * time.Second
	quarantinePublishTTL   = 2 * time.Second
)

type Disposition int

const (
	DispositionAck Disposition = iota
	DispositionNak
	DispositionTerm
)

type ConsumerConfig struct {
	URL             string
	StreamName      string
	DedupWindow     time.Duration
	MaxAge          time.Duration
	MaxBytes        int64
	ConsumerDurable string
	FilterSubjects  []string
	AckWait         time.Duration
	MaxAckPending   int
	MaxDeliver      int
	DeliverPolicy   string
	FetchTimeout    time.Duration
	LagPollInterval time.Duration
}

type ConsumeHandler func(ctx context.Context, env envelope.Envelope) *problem.Problem

type ackDispositionMessage interface {
	Ack(...nats.AckOpt) error
	Nak(...nats.AckOpt) error
	Term(...nats.AckOpt) error
}

type Consumer struct {
	nc                   *nats.Conn
	js                   nats.JetStreamContext
	cfg                  ConsumerConfig
	sub                  *nats.Subscription
	observer             observability.BusObserver
	transientRetryBudget int
	retryBudget          *retryBudgetTracker
}

func NewConsumer(ctx context.Context, cfg ConsumerConfig, observer observability.BusObserver) (*Consumer, *problem.Problem) {
	cfg = withConsumerDefaults(cfg)
	if p := validateConsumerConfig(cfg); p != nil {
		return nil, p
	}
	if observer == nil {
		observer = observability.NopBusObserver()
	}

	nc, err := nats.Connect(
		cfg.URL,
		nats.Name("market-raccoon-jetstream-consumer"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, wrapUnavailable("connect_failed", err, "jetstream consumer connect failed")
	}
	js, err := nc.JetStream()
	if err != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, wrapUnavailable("context_failed", err, "jetstream consumer context create failed")
	}

	streamCfg := PublisherConfig{
		URL:         cfg.URL,
		StreamName:  cfg.StreamName,
		DedupWindow: cfg.DedupWindow,
		MaxAge:      cfg.MaxAge,
		MaxBytes:    cfg.MaxBytes,
	}
	if p := ensureStream(ctx, js, streamCfg); p != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, p
	}

	consumerCfg, p := toNATSConsumerConfig(cfg)
	if p != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, p
	}
	if _, err = js.AddConsumer(cfg.StreamName, consumerCfg, nats.Context(ctx)); err != nil {
		if !errors.Is(err, nats.ErrConsumerNameAlreadyInUse) {
			_ = nc.Drain()
			nc.Close()
			return nil, wrapUnavailable("consumer_create_failed", err, "jetstream consumer create failed")
		}
		if _, updateErr := js.UpdateConsumer(cfg.StreamName, consumerCfg, nats.Context(ctx)); updateErr != nil {
			_ = nc.Drain()
			nc.Close()
			return nil, wrapUnavailable("consumer_update_failed", updateErr, "jetstream consumer update failed")
		}
	}

	sub, err := js.PullSubscribe(
		cfg.FilterSubjects[0],
		cfg.ConsumerDurable,
		nats.Bind(cfg.StreamName, cfg.ConsumerDurable),
	)
	if err != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, wrapUnavailable("subscribe_failed", err, "jetstream pull subscribe failed")
	}

	return &Consumer{
		nc:                   nc,
		js:                   js,
		cfg:                  cfg,
		sub:                  sub,
		observer:             observer,
		transientRetryBudget: withTransientRetryBudget(cfg.MaxDeliver),
		retryBudget:          newRetryBudgetTracker(defaultRetryBudgetFallbackCapacity),
	}, nil
}

func (c *Consumer) Consume(ctx context.Context, handler ConsumeHandler) *problem.Problem {
	if handler == nil {
		return problem.New(problem.ValidationFailed, "jetstream consume handler must not be nil")
	}

	lagTicker := time.NewTicker(c.cfg.LagPollInterval)
	defer lagTicker.Stop()
	defer c.updateLag()

	for {
		if ctx.Err() != nil {
			return nil
		}

		msgs, err := c.sub.Fetch(1, nats.MaxWait(c.cfg.FetchTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				select {
				case <-lagTicker.C:
					c.updateLag()
				default:
				}
				continue
			}
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil
			}
			c.observer.IncConsumed(busTypeJetStream, "error")
			return wrapUnavailable("fetch_failed", err, "jetstream fetch failed")
		}

		for _, msg := range msgs {
			if p := c.consumeOne(ctx, msg, handler); p != nil {
				return p
			}
		}

		select {
		case <-lagTicker.C:
			c.updateLag()
		default:
		}
	}
}

func (c *Consumer) consumeOne(ctx context.Context, msg *nats.Msg, handler ConsumeHandler) *problem.Problem {
	meta, _ := msg.Metadata()
	if meta != nil && meta.NumDelivered > 1 {
		c.observer.IncRedelivered(busTypeJetStream)
	}

	env, decodeProb := envelope.UnmarshalBinary(msg.Data)
	if decodeProb != nil {
		decision := classifyEnvelopeDecodeFailure(decodeProb)
		if decision.Quarantine && !isQuarantineMessage(msg, envelope.Envelope{}) {
			decision = applyQuarantinePublishResult(decision, c.publishQuarantine(ctx, msg, envelope.Envelope{}, decision.ReasonCode, decodeProb))
		}
		return c.ackWithDisposition(ctx, msg, decision.Disposition, decision.Status, decision.ReasonCode, time.Now())
	}

	stopHeartbeat := startAckHeartbeat(
		ctx,
		c.cfg.AckWait,
		msg.InProgress,
		func(error) {
			c.observer.IncConsumed(busTypeJetStream, "heartbeat_error")
		},
	)
	defer stopHeartbeat()

	started := time.Now()
	procProb := handler(ctx, env)
	stopHeartbeat()
	if ctx.Err() != nil {
		// Shutdown path: do not ack/nak/term. Let JetStream redeliver.
		return nil
	}

	decision := ClassifyIngestError(procProb, env)
	if decision.Quarantine && !isQuarantineMessage(msg, env) {
		decision = applyQuarantinePublishResult(decision, c.publishQuarantine(ctx, msg, env, decision.ReasonCode, procProb))
	}
	decision = c.applyTransientRetryBudget(msg, env, meta, decision)
	return c.ackWithDisposition(ctx, msg, decision.Disposition, decision.Status, decision.ReasonCode, started)
}

func startAckHeartbeat(
	ctx context.Context,
	ackWait time.Duration,
	inProgressFn func(...nats.AckOpt) error,
	onHeartbeatError func(error),
) func() {
	if inProgressFn == nil {
		return func() {}
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	interval := heartbeatInterval(ackWait)
	ticker := time.NewTicker(interval)

	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := inProgressFn(); err != nil && onHeartbeatError != nil {
					onHeartbeatError(err)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func heartbeatInterval(ackWait time.Duration) time.Duration {
	interval := ackWait / 3
	if interval < minHeartbeatInterval {
		return minHeartbeatInterval
	}
	if interval > maxHeartbeatInterval {
		return maxHeartbeatInterval
	}
	return interval
}

func (c *Consumer) ackWithDisposition(ctx context.Context, msg ackDispositionMessage, disposition Disposition, status, reasonCode string, startedAt time.Time) *problem.Problem {
	if ctx.Err() != nil {
		return nil
	}
	if msg == nil {
		return problem.New(problem.ValidationFailed, "jetstream ack message must not be nil")
	}

	var ackErr error
	switch disposition {
	case DispositionAck:
		ackErr = msg.Ack()
	case DispositionNak:
		ackErr = msg.Nak()
	case DispositionTerm:
		ackErr = msg.Term()
	default:
		ackErr = msg.Nak()
		status = "nak"
	}
	c.observer.ObserveAckLatency(busTypeJetStream, time.Since(startedAt))

	if ackErr != nil {
		c.observer.IncConsumed(busTypeJetStream, "error")
		return wrapUnavailable("ack_failed", ackErr, "jetstream ack operation failed")
	}
	c.observer.IncConsumed(busTypeJetStream, status)
	recordIngestDecisionMetrics(disposition, reasonCode)
	return nil
}

func (c *Consumer) Close(ctx context.Context) *problem.Problem {
	if c == nil || c.nc == nil {
		return nil
	}
	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}

	closeCtx := ctx
	if closeCtx == nil {
		var cancel context.CancelFunc
		closeCtx, cancel = context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()
	}

	done := make(chan error, 1)
	go func() {
		done <- c.nc.Drain()
	}()

	select {
	case err := <-done:
		c.nc.Close()
		if err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
			return wrapUnavailable("drain_failed", err, "jetstream consumer drain failed")
		}
		return nil
	case <-closeCtx.Done():
		c.nc.Close()
		return wrapUnavailable("drain_timeout", closeCtx.Err(), "jetstream consumer drain timed out")
	}
}

func MapProblemToDisposition(p *problem.Problem) (Disposition, string) {
	decision := ClassifyIngestError(p, envelope.Envelope{})
	return decision.Disposition, decision.Status
}

func toNATSConsumerConfig(cfg ConsumerConfig) (*nats.ConsumerConfig, *problem.Problem) {
	deliverPolicy, p := mapDeliverPolicy(cfg.DeliverPolicy)
	if p != nil {
		return nil, p
	}

	ccfg := &nats.ConsumerConfig{
		Durable:       cfg.ConsumerDurable,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverPolicy: deliverPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
		MaxAckPending: cfg.MaxAckPending,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
	}
	if len(cfg.FilterSubjects) == 1 {
		ccfg.FilterSubject = cfg.FilterSubjects[0]
	} else {
		ccfg.FilterSubjects = append([]string(nil), cfg.FilterSubjects...)
	}
	return ccfg, nil
}

func mapDeliverPolicy(policy string) (nats.DeliverPolicy, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "all":
		return nats.DeliverAllPolicy, nil
	case "new":
		return nats.DeliverNewPolicy, nil
	case "last":
		return nats.DeliverLastPolicy, nil
	default:
		return nats.DeliverAllPolicy, problem.Newf(problem.ValidationFailed, "unsupported deliver policy %q", policy)
	}
}

func withConsumerDefaults(cfg ConsumerConfig) ConsumerConfig {
	cfg = withDefaults(PublisherConfig{
		URL:            cfg.URL,
		StreamName:     cfg.StreamName,
		DedupWindow:    cfg.DedupWindow,
		MaxAge:         cfg.MaxAge,
		MaxBytes:       cfg.MaxBytes,
		PublishTimeout: defaultPublishTimeout,
	}).toConsumerDefaults(cfg)
	if cfg.ConsumerDurable == "" {
		cfg.ConsumerDurable = "processor-v1"
	}
	if cfg.AckWait <= 0 {
		cfg.AckWait = 30 * time.Second
	}
	if cfg.MaxAckPending <= 0 {
		cfg.MaxAckPending = 1024
	}
	if cfg.MaxDeliver <= 0 {
		cfg.MaxDeliver = 10
	}
	if cfg.DeliverPolicy == "" {
		cfg.DeliverPolicy = "all"
	}
	if len(cfg.FilterSubjects) == 0 {
		cfg.FilterSubjects = []string{"marketdata.bookdelta.>"}
	}
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = defaultFetchTimeout
	}
	if cfg.LagPollInterval <= 0 {
		cfg.LagPollInterval = defaultLagPollInterval
	}
	return cfg
}

func (p PublisherConfig) toConsumerDefaults(cfg ConsumerConfig) ConsumerConfig {
	cfg.URL = p.URL
	cfg.StreamName = p.StreamName
	cfg.DedupWindow = p.DedupWindow
	cfg.MaxAge = p.MaxAge
	cfg.MaxBytes = p.MaxBytes
	return cfg
}

func validateConsumerConfig(cfg ConsumerConfig) *problem.Problem {
	if p := validateConsumerRequired(cfg); p != nil {
		return p
	}
	if p := validateConsumerPositive(cfg); p != nil {
		return p
	}
	if _, p := mapDeliverPolicy(cfg.DeliverPolicy); p != nil {
		return p
	}
	return nil
}

func validateConsumerRequired(cfg ConsumerConfig) *problem.Problem {
	if strings.TrimSpace(cfg.URL) == "" {
		return problem.New(problem.ValidationFailed, "jetstream consumer url must not be empty")
	}
	if strings.TrimSpace(cfg.StreamName) == "" {
		return problem.New(problem.ValidationFailed, "jetstream consumer stream_name must not be empty")
	}
	if strings.TrimSpace(cfg.ConsumerDurable) == "" {
		return problem.New(problem.ValidationFailed, "jetstream consumer durable must not be empty")
	}
	if len(cfg.FilterSubjects) == 0 {
		return problem.New(problem.ValidationFailed, "jetstream consumer filter_subjects must not be empty")
	}
	for i, s := range cfg.FilterSubjects {
		if strings.TrimSpace(s) == "" {
			return problem.Newf(problem.ValidationFailed, "jetstream consumer filter_subjects[%d] must not be empty", i)
		}
		if err := ValidateSubjectPattern(s); err != nil {
			return problem.Newf(problem.ValidationFailed, "jetstream consumer filter_subjects[%d] invalid: %v", i, err)
		}
	}
	return nil
}

func validateConsumerPositive(cfg ConsumerConfig) *problem.Problem {
	if cfg.AckWait <= 0 || cfg.MaxAckPending <= 0 || cfg.MaxDeliver <= 0 || cfg.FetchTimeout <= 0 || cfg.LagPollInterval <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream consumer config has non-positive values")
	}
	if cfg.MaxBytes <= 0 || cfg.MaxAge <= 0 || cfg.DedupWindow <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream consumer stream config has non-positive values")
	}
	return nil
}

func (c *Consumer) updateLag() {
	if c == nil || c.sub == nil {
		return
	}
	info, err := c.sub.ConsumerInfo()
	if err != nil || info == nil {
		return
	}
	lag := info.NumPending
	const maxInt64 = uint64(1<<63 - 1)
	if lag > maxInt64 {
		c.observer.SetConsumerLag(busTypeJetStream, 1<<63-1)
		return
	}
	c.observer.SetConsumerLag(busTypeJetStream, int64(lag))
}

func (c *Consumer) publishQuarantine(ctx context.Context, msg *nats.Msg, env envelope.Envelope, reasonCode string, procProb *problem.Problem) *problem.Problem {
	if c == nil || c.js == nil || msg == nil {
		return problem.WithRetryable(problem.New(problem.Unavailable, "jetstream quarantine publisher unavailable"))
	}
	out, p := buildQuarantineEnvelope(msg, env, reasonCode, procProb)
	if p != nil {
		return p
	}
	data, p := envelope.MarshalBinary(out)
	if p != nil {
		return p
	}

	quarantineMsg := nats.NewMsg(envelope.SubjectFromEnvelope(out))
	quarantineMsg.Data = data
	quarantineMsg.Header.Set(nats.MsgIdHdr, out.IdempotencyKey)

	pubCtx, cancel := context.WithTimeout(ctx, quarantinePublishTTL)
	defer cancel()

	_, err := c.js.PublishMsg(quarantineMsg, nats.Context(pubCtx))
	if err != nil {
		kind := classifyPublishError(err)
		retryable, classifiedReason := ClassifyQuarantinePublishError(err)
		if classifiedReason == "" {
			classifiedReason = ingestReasonQuarantinePublishError
		}
		out := problem.WithDetail(
			problem.WithDetail(problem.Wrap(err, problem.Unavailable, "jetstream quarantine publish failed"), "kind", kind),
			"reason_code", classifiedReason,
		)
		if retryable {
			return problem.WithRetryable(out)
		}
		slog.Warn(
			"jetstream: permanent quarantine publish failure, terminating poison message",
			"reason_code", classifiedReason,
			"kind", kind,
			"subject", strings.TrimSpace(quarantineMsg.Subject),
		)
		return out
	}
	metrics.IncIngestQuarantine(reasonCode)
	return nil
}

func recordIngestDecisionMetrics(disposition Disposition, reasonCode string) {
	switch disposition {
	case DispositionAck:
		if normalizeIngestReason(reasonCode) != ingestReasonOK {
			metrics.IncIngestDrop(reasonCode)
		}
	case DispositionNak:
		metrics.IncIngestNak(reasonCode)
	case DispositionTerm:
		metrics.IncIngestTerm(reasonCode)
	}
}
