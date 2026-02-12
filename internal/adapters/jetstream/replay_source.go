package jetstream

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const (
	replayDeliverAll         = "all"
	replayDeliverByStartTime = "by_start_time"
	replayDecodeErrorFail    = "fail"
	replayDecodeErrorSkip    = "skip"

	defaultReplayFetchTimeout    = 750 * time.Millisecond
	defaultReplayIdleTimeouts    = 2
	defaultReplayMergeBuffer     = 4096
	defaultReplayOutputBuffer    = 256
	defaultReplayMaxMessages     = 100_000
	maxReplayMaxMessages         = 10_000_000
	defaultReplayConsumerDurable = "processor-replay-v1"
)

// ReplaySourceConfig defines deterministic replay read behavior from JetStream.
type ReplaySourceConfig struct {
	URL             string
	StreamName      string
	SubjectFilter   string
	ConsumerDurable string

	DedupWindow time.Duration
	MaxAge      time.Duration
	MaxBytes    int64

	AckWait       time.Duration
	MaxAckPending int
	MaxDeliver    int

	DeliverPolicy    string
	Window           time.Duration
	MaxMessages      int
	FetchTimeout     time.Duration
	IdleTimeoutLimit int
	MergeBufferSize  int
	OutputBufferSize int
	DecodeErrorMode  string
}

// Source yields envelopes from JetStream for deterministic replay.
type Source struct {
	nc  *nats.Conn
	js  nats.JetStreamContext
	cfg ReplaySourceConfig

	mu     sync.Mutex
	closed bool
}

// NewJetStreamReplaySource creates a replay source backed by JetStream pull consumer.
func NewJetStreamReplaySource(cfg ReplaySourceConfig) (*Source, *problem.Problem) {
	cfg = withReplaySourceDefaults(cfg)
	if p := validateReplaySourceConfig(cfg); p != nil {
		return nil, p
	}

	nc, err := nats.Connect(
		cfg.URL,
		nats.Name("market-raccoon-jetstream-replay-source"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, wrapUnavailable("connect_failed", err, "jetstream replay source connect failed")
	}

	js, err := nc.JetStream()
	if err != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, wrapUnavailable("context_failed", err, "jetstream replay source context create failed")
	}

	if p := ensureStream(context.Background(), js, PublisherConfig{
		URL:         cfg.URL,
		StreamName:  cfg.StreamName,
		DedupWindow: cfg.DedupWindow,
		MaxAge:      cfg.MaxAge,
		MaxBytes:    cfg.MaxBytes,
	}); p != nil {
		_ = nc.Drain()
		nc.Close()
		return nil, p
	}

	return &Source{
		nc:  nc,
		js:  js,
		cfg: cfg,
	}, nil
}

// Read starts a deterministic read stream from JetStream.
// It returns envelope channel and a close function that joins the internal loop.
func (s *Source) Read(ctx context.Context) (<-chan envelope.Envelope, func() error, *problem.Problem) {
	if s == nil {
		return nil, nil, problem.New(problem.ValidationFailed, "jetstream replay source must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	startNow := time.Now()
	ccfg, p := s.buildConsumerConfig(startNow)
	if p != nil {
		return nil, nil, p
	}
	if _, err := s.js.AddConsumer(s.cfg.StreamName, ccfg, nats.Context(ctx)); err != nil {
		if !errors.Is(err, nats.ErrConsumerNameAlreadyInUse) {
			return nil, nil, wrapUnavailable("consumer_create_failed", err, "jetstream replay source consumer create failed")
		}
		if _, updateErr := s.js.UpdateConsumer(s.cfg.StreamName, ccfg, nats.Context(ctx)); updateErr != nil {
			return nil, nil, wrapUnavailable("consumer_update_failed", updateErr, "jetstream replay source consumer update failed")
		}
	}

	sub, err := s.js.PullSubscribe(
		s.cfg.SubjectFilter,
		s.cfg.ConsumerDurable,
		nats.Bind(s.cfg.StreamName, s.cfg.ConsumerDurable),
	)
	if err != nil {
		return nil, nil, wrapUnavailable("subscribe_failed", err, "jetstream replay source pull subscribe failed")
	}

	out := make(chan envelope.Envelope, s.cfg.OutputBufferSize)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		defer close(out)
		defer func() {
			_ = sub.Unsubscribe()
		}()
		done <- s.readLoop(runCtx, sub, out, startNow)
	}()

	var closeOnce sync.Once
	closeFn := func() error {
		var joinErr error
		closeOnce.Do(func() {
			cancel()
			joinErr = <-done
			if closeErr := s.close(context.Background()); closeErr != nil && joinErr == nil {
				joinErr = closeErr
			}
		})
		return joinErr
	}

	return out, closeFn, nil
}

//nolint:gocyclo // Replay loop keeps protocol decisions localized to preserve deterministic behavior.
func (s *Source) readLoop(ctx context.Context, sub *nats.Subscription, out chan<- envelope.Envelope, startNow time.Time) error {
	order := newEnvelopeHeap(s.cfg.MergeBufferSize)
	h := &order.items
	heap.Init(h)

	cutoffMillis := startNow.UnixMilli()
	windowStartMillis := int64(0)
	if s.cfg.Window > 0 {
		windowStartMillis = startNow.Add(-s.cfg.Window).UnixMilli()
	}

	var lastEmitted envelope.Envelope
	hasLast := false
	emitted := 0
	idleTimeouts := 0

	emitOne := func() error {
		if h.Len() == 0 {
			return nil
		}
		item := heap.Pop(h).(orderedMsg)
		if hasLast && envelopeLess(item.env, lastEmitted) {
			return wrapUnavailable(
				"merge_buffer_overflow",
				nil,
				"jetstream replay deterministic ordering violated: increase merge buffer or reduce disorder",
			)
		}

		select {
		case <-ctx.Done():
			return nil
		case out <- item.env:
		}

		if err := item.msg.Ack(); err != nil {
			return wrapUnavailable("ack_failed", err, "jetstream replay source ack failed")
		}

		hasLast = true
		lastEmitted = item.env
		emitted++
		metrics.IncReplayMessages("jetstream", "ok")
		metrics.ObserveReplayLatency("jetstream", time.Since(time.UnixMilli(item.env.TsIngest)))
		if item.redelivered {
			metrics.IncReplayRedeliveries("jetstream")
		}
		return nil
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		if s.cfg.MaxMessages > 0 && emitted >= s.cfg.MaxMessages {
			return nil
		}
		if s.cfg.MaxMessages > 0 && emitted+h.Len() >= s.cfg.MaxMessages {
			if err := emitOne(); err != nil {
				return err
			}
			continue
		}

		msgs, err := sub.Fetch(1, nats.MaxWait(s.cfg.FetchTimeout))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) {
				idleTimeouts++
				if h.Len() > 0 {
					if emitErr := emitOne(); emitErr != nil {
						return emitErr
					}
					continue
				}
				if idleTimeouts >= s.cfg.IdleTimeoutLimit {
					pending, p := pendingMessages(sub)
					if p != nil {
						return p
					}
					if pending == 0 {
						for h.Len() > 0 {
							if emitErr := emitOne(); emitErr != nil {
								return emitErr
							}
						}
						return nil
					}
				}
				continue
			}
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return wrapUnavailable("fetch_failed", err, "jetstream replay source fetch failed")
		}
		idleTimeouts = 0

		for _, msg := range msgs {
			meta, _ := msg.Metadata()
			env, decodeProb := envelope.UnmarshalBinary(msg.Data)
			if decodeProb != nil {
				if strings.EqualFold(s.cfg.DecodeErrorMode, replayDecodeErrorSkip) {
					if ackErr := msg.Ack(); ackErr != nil {
						return wrapUnavailable("ack_failed", ackErr, "jetstream replay source ack failed")
					}
					metrics.IncReplayMessages("jetstream", "decode_skip")
					continue
				}
				metrics.IncReplayMessages("jetstream", "decode_fail")
				return decodeProb
			}

			if windowStartMillis > 0 {
				if env.TsIngest < windowStartMillis || env.TsIngest > cutoffMillis {
					if ackErr := msg.Ack(); ackErr != nil {
						return wrapUnavailable("ack_failed", ackErr, "jetstream replay source ack failed")
					}
					metrics.IncReplayMessages("jetstream", "window_skip")
					continue
				}
			}

			if hasLast && envelopeLess(env, lastEmitted) {
				return wrapUnavailable(
					"merge_buffer_overflow",
					nil,
					"jetstream replay deterministic ordering overflow; message arrived behind emitted frontier",
				)
			}

			heap.Push(h, orderedMsg{env: env, msg: msg, redelivered: meta != nil && meta.NumDelivered > 1})
			if h.Len() > order.maxSize {
				if emitErr := emitOne(); emitErr != nil {
					return emitErr
				}
			}
		}
	}
}

func pendingMessages(sub *nats.Subscription) (uint64, *problem.Problem) {
	info, err := sub.ConsumerInfo()
	if err != nil {
		return 0, wrapUnavailable("consumer_info_failed", err, "jetstream replay source consumer info failed")
	}
	if info == nil {
		return 0, nil
	}
	return info.NumPending, nil
}

func (s *Source) buildConsumerConfig(now time.Time) (*nats.ConsumerConfig, *problem.Problem) {
	deliverPolicy, p := replayDeliverPolicy(s.cfg.DeliverPolicy)
	if p != nil {
		return nil, p
	}

	ccfg := &nats.ConsumerConfig{
		Durable:       s.cfg.ConsumerDurable,
		AckPolicy:     nats.AckExplicitPolicy,
		DeliverPolicy: deliverPolicy,
		ReplayPolicy:  nats.ReplayInstantPolicy,
		MaxAckPending: s.cfg.MaxAckPending,
		AckWait:       s.cfg.AckWait,
		MaxDeliver:    s.cfg.MaxDeliver,
		FilterSubject: s.cfg.SubjectFilter,
	}
	if deliverPolicy == nats.DeliverByStartTimePolicy {
		start := now.Add(-s.cfg.Window)
		ccfg.OptStartTime = &start
	}
	return ccfg, nil
}

func replayDeliverPolicy(policy string) (nats.DeliverPolicy, *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case replayDeliverAll:
		return nats.DeliverAllPolicy, nil
	case replayDeliverByStartTime:
		return nats.DeliverByStartTimePolicy, nil
	default:
		return nats.DeliverAllPolicy, problem.Newf(problem.ValidationFailed, "unsupported replay deliver policy %q", policy)
	}
}

func withReplaySourceDefaults(cfg ReplaySourceConfig) ReplaySourceConfig {
	base := withConsumerDefaults(ConsumerConfig{
		URL:             cfg.URL,
		StreamName:      cfg.StreamName,
		DedupWindow:     cfg.DedupWindow,
		MaxAge:          cfg.MaxAge,
		MaxBytes:        cfg.MaxBytes,
		ConsumerDurable: cfg.ConsumerDurable,
		FilterSubjects:  []string{cfg.SubjectFilter},
		AckWait:         cfg.AckWait,
		MaxAckPending:   cfg.MaxAckPending,
		MaxDeliver:      cfg.MaxDeliver,
		DeliverPolicy:   "all",
	})

	cfg.URL = base.URL
	cfg.StreamName = base.StreamName
	cfg.DedupWindow = base.DedupWindow
	cfg.MaxAge = base.MaxAge
	cfg.MaxBytes = base.MaxBytes
	cfg.AckWait = base.AckWait
	cfg.MaxAckPending = base.MaxAckPending
	cfg.MaxDeliver = base.MaxDeliver
	if strings.TrimSpace(cfg.SubjectFilter) == "" {
		cfg.SubjectFilter = subjectWildcard
	}
	if strings.TrimSpace(cfg.ConsumerDurable) == "" {
		cfg.ConsumerDurable = defaultReplayConsumerDurable
	}
	if cfg.FetchTimeout <= 0 {
		cfg.FetchTimeout = defaultReplayFetchTimeout
	}
	if cfg.IdleTimeoutLimit <= 0 {
		cfg.IdleTimeoutLimit = defaultReplayIdleTimeouts
	}
	if cfg.MergeBufferSize <= 0 {
		cfg.MergeBufferSize = defaultReplayMergeBuffer
	}
	if cfg.OutputBufferSize <= 0 {
		cfg.OutputBufferSize = defaultReplayOutputBuffer
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = defaultReplayMaxMessages
	}
	if strings.TrimSpace(cfg.DeliverPolicy) == "" {
		if cfg.Window > 0 {
			cfg.DeliverPolicy = replayDeliverByStartTime
		} else {
			cfg.DeliverPolicy = replayDeliverAll
		}
	}
	if strings.TrimSpace(cfg.DecodeErrorMode) == "" {
		cfg.DecodeErrorMode = replayDecodeErrorFail
	}
	return cfg
}

//nolint:gocyclo // Validation is explicit per field to keep operator-facing errors precise.
func validateReplaySourceConfig(cfg ReplaySourceConfig) *problem.Problem {
	if strings.TrimSpace(cfg.URL) == "" {
		return problem.New(problem.ValidationFailed, "jetstream replay source url must not be empty")
	}
	if strings.TrimSpace(cfg.StreamName) == "" {
		return problem.New(problem.ValidationFailed, "jetstream replay source stream_name must not be empty")
	}
	if strings.TrimSpace(cfg.SubjectFilter) == "" {
		return problem.New(problem.ValidationFailed, "jetstream replay source subject_filter must not be empty")
	}
	if strings.TrimSpace(cfg.ConsumerDurable) == "" {
		return problem.New(problem.ValidationFailed, "jetstream replay source consumer_durable must not be empty")
	}
	if cfg.AckWait <= 0 || cfg.MaxAckPending <= 0 || cfg.MaxDeliver <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream replay source consumer config has non-positive values")
	}
	if cfg.FetchTimeout <= 0 || cfg.IdleTimeoutLimit <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream replay source read config has non-positive values")
	}
	if cfg.MergeBufferSize <= 0 || cfg.OutputBufferSize <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream replay source buffer config has non-positive values")
	}
	if cfg.MaxMessages <= 0 || cfg.MaxMessages > maxReplayMaxMessages {
		return problem.Newf(problem.ValidationFailed, "jetstream replay source max_messages must be in [1,%d], got %d", maxReplayMaxMessages, cfg.MaxMessages)
	}
	if cfg.MaxBytes <= 0 || cfg.MaxAge <= 0 || cfg.DedupWindow <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream replay source stream config has non-positive values")
	}
	if _, p := replayDeliverPolicy(cfg.DeliverPolicy); p != nil {
		return p
	}
	if strings.EqualFold(strings.TrimSpace(cfg.DeliverPolicy), replayDeliverByStartTime) && cfg.Window <= 0 {
		return problem.New(problem.ValidationFailed, "jetstream replay source window must be > 0 when deliver_policy=by_start_time")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.DecodeErrorMode)) {
	case replayDecodeErrorFail, replayDecodeErrorSkip:
	default:
		return problem.Newf(problem.ValidationFailed, "jetstream replay source decode_error_mode must be fail|skip, got %q", cfg.DecodeErrorMode)
	}
	return nil
}

func (s *Source) close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	nc := s.nc
	s.mu.Unlock()

	if nc == nil {
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
		done <- nc.Drain()
	}()

	select {
	case err := <-done:
		nc.Close()
		if err != nil && !errors.Is(err, nats.ErrConnectionClosed) {
			return fmt.Errorf("drain replay source failed: %w", err)
		}
		return nil
	case <-closeCtx.Done():
		nc.Close()
		return fmt.Errorf("drain replay source timeout: %w", closeCtx.Err())
	}
}

type orderedMsg struct {
	env         envelope.Envelope
	msg         *nats.Msg
	redelivered bool
}

type envelopeHeap struct {
	maxSize int
	items   orderedMsgHeap
}

func newEnvelopeHeap(maxSize int) envelopeHeap {
	if maxSize <= 0 {
		maxSize = defaultReplayMergeBuffer
	}
	return envelopeHeap{maxSize: maxSize}
}

type orderedMsgHeap []orderedMsg

func (h orderedMsgHeap) Len() int           { return len(h) }
func (h orderedMsgHeap) Less(i, j int) bool { return envelopeLess(h[i].env, h[j].env) }
func (h orderedMsgHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *orderedMsgHeap) Push(x any) {
	*h = append(*h, x.(orderedMsg))
}

func (h *orderedMsgHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func envelopeLess(a, b envelope.Envelope) bool {
	if a.TsIngest != b.TsIngest {
		return a.TsIngest < b.TsIngest
	}
	aVenue := strings.ToLower(strings.TrimSpace(a.Venue))
	bVenue := strings.ToLower(strings.TrimSpace(b.Venue))
	if aVenue != bVenue {
		return aVenue < bVenue
	}
	aInstrument := strings.ToUpper(strings.TrimSpace(a.Instrument))
	bInstrument := strings.ToUpper(strings.TrimSpace(b.Instrument))
	if aInstrument != bInstrument {
		return aInstrument < bInstrument
	}
	aType := strings.ToLower(strings.TrimSpace(a.Type))
	bType := strings.ToLower(strings.TrimSpace(b.Type))
	if aType != bType {
		return aType < bType
	}
	if a.Seq != b.Seq {
		return a.Seq < b.Seq
	}
	aID := strings.TrimSpace(a.IdempotencyKey)
	bID := strings.TrimSpace(b.IdempotencyKey)
	return aID < bID
}
