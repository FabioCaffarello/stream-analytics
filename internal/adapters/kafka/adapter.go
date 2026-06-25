package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/market-raccoon/internal/application/dataplane"
	"github.com/market-raccoon/internal/shared/problem"
	kafkago "github.com/segmentio/kafka-go"
)

type Message struct {
	Topic     string
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Partition int
	Offset    int64
	Time      time.Time
}

type ReaderConfig struct {
	Brokers     []string
	GroupID     string
	Topics      []string
	MinBytes    int
	MaxBytes    int
	MaxWait     time.Duration
	StartOffset int64
}

type Reader struct {
	reader *kafkago.Reader
}

func NewReader(cfg ReaderConfig) (*Reader, *problem.Problem) {
	if len(cfg.Brokers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "kafka brokers must not be empty")
	}
	if strings.TrimSpace(cfg.GroupID) == "" {
		return nil, problem.New(problem.ValidationFailed, "kafka group_id must not be empty")
	}
	if len(cfg.Topics) == 0 {
		return nil, problem.New(problem.ValidationFailed, "kafka topics must not be empty")
	}
	if cfg.MinBytes <= 0 {
		cfg.MinBytes = 1
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = 10e6
	}
	if cfg.MaxWait <= 0 {
		cfg.MaxWait = time.Second
	}
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     append([]string(nil), cfg.Brokers...),
		GroupID:     cfg.GroupID,
		GroupTopics: append([]string(nil), cfg.Topics...),
		MinBytes:    cfg.MinBytes,
		MaxBytes:    cfg.MaxBytes,
		MaxWait:     cfg.MaxWait,
		StartOffset: cfg.StartOffset,
	})
	return &Reader{reader: reader}, nil
}

func (r *Reader) Fetch(ctx context.Context) (Message, *problem.Problem) {
	if r == nil || r.reader == nil {
		return Message{}, problem.New(problem.Internal, "kafka reader must not be nil")
	}
	msg, err := r.reader.FetchMessage(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Message{}, problem.Wrap(err, problem.Unavailable, "kafka fetch canceled")
		}
		return Message{}, problem.Wrap(err, problem.Unavailable, "kafka fetch failed")
	}
	headers := make(map[string]string, len(msg.Headers))
	for _, header := range msg.Headers {
		headers[header.Key] = string(header.Value)
	}
	return Message{
		Topic:     msg.Topic,
		Key:       append([]byte(nil), msg.Key...),
		Value:     append([]byte(nil), msg.Value...),
		Headers:   headers,
		Partition: msg.Partition,
		Offset:    msg.Offset,
		Time:      msg.Time,
	}, nil
}

func (r *Reader) Commit(ctx context.Context, msg Message) *problem.Problem {
	if r == nil || r.reader == nil {
		return problem.New(problem.Internal, "kafka reader must not be nil")
	}
	err := r.reader.CommitMessages(ctx, kafkago.Message{
		Topic:     msg.Topic,
		Partition: msg.Partition,
		Offset:    msg.Offset,
		Time:      msg.Time,
	})
	if err != nil {
		return problem.Wrap(err, problem.Unavailable, "kafka commit failed")
	}
	return nil
}

func (r *Reader) Close() error {
	if r == nil || r.reader == nil {
		return nil
	}
	return r.reader.Close()
}

type WriterConfig struct {
	Brokers      []string
	BatchTimeout time.Duration
}

type Writer struct {
	writer *kafkago.Writer
}

func NewWriter(cfg WriterConfig) (*Writer, *problem.Problem) {
	if len(cfg.Brokers) == 0 {
		return nil, problem.New(problem.ValidationFailed, "kafka brokers must not be empty")
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = 250 * time.Millisecond
	}
	return &Writer{
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(cfg.Brokers...),
			Balancer:     &kafkago.LeastBytes{},
			BatchTimeout: cfg.BatchTimeout,
		},
	}, nil
}

func (w *Writer) Write(ctx context.Context, topic string, key, value []byte, headers map[string]string) *problem.Problem {
	if w == nil || w.writer == nil {
		return problem.New(problem.Internal, "kafka writer must not be nil")
	}
	if strings.TrimSpace(topic) == "" {
		return problem.New(problem.ValidationFailed, "kafka topic must not be empty")
	}
	kafkaHeaders := make([]kafkago.Header, 0, len(headers))
	for k, v := range headers {
		kafkaHeaders = append(kafkaHeaders, kafkago.Header{Key: k, Value: []byte(v)})
	}
	if err := w.writer.WriteMessages(ctx, kafkago.Message{
		Topic:   topic,
		Key:     append([]byte(nil), key...),
		Value:   append([]byte(nil), value...),
		Headers: kafkaHeaders,
	}); err != nil {
		return problem.Wrap(err, problem.Unavailable, "kafka write failed")
	}
	return nil
}

func (w *Writer) Close() error {
	if w == nil || w.writer == nil {
		return nil
	}
	return w.writer.Close()
}

func MapToDataPlaneMessage(binding dataplane.Binding, msg Message) (dataplane.Message, *problem.Problem) {
	if p := binding.Validate(); p != nil {
		return dataplane.Message{}, p
	}
	if strings.TrimSpace(msg.Topic) == "" {
		return dataplane.Message{}, problem.New(problem.ValidationFailed, "kafka message topic must not be empty")
	}
	if msg.Topic != binding.KafkaTopic {
		return dataplane.Message{}, problem.Newf(problem.ValidationFailed, "kafka topic %q does not match binding topic %q", msg.Topic, binding.KafkaTopic)
	}
	if _, p := dataplane.DecodePayloadObject(msg.Value); p != nil {
		return dataplane.Message{}, p
	}
	messageID := strings.TrimSpace(msg.Headers["message_id"])
	if messageID == "" {
		messageID = uuid.NewString()
	}
	correlationID := strings.TrimSpace(msg.Headers["correlation_id"])
	producedAt := msg.Time.UnixMilli()
	if producedAt <= 0 {
		producedAt = time.Now().UnixMilli()
	}
	return dataplane.Message{
		MessageID:     messageID,
		Binding:       binding.Name,
		Topic:         msg.Topic,
		CorrelationID: correlationID,
		ProducedAt:    producedAt,
		Payload:       json.RawMessage(append([]byte(nil), msg.Value...)),
	}, nil
}
