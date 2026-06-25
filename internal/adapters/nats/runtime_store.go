package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/market-raccoon/internal/application/dataplane"
	"github.com/market-raccoon/internal/application/runtimebootstrap"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/nats-io/nats.go"
)

const (
	bindingKeyPrefix    = "bindings."
	configKeyPrefix     = "configs."
	activationKeyPrefix = "active."
)

type RuntimeStore struct {
	nc *nats.Conn
	kv nats.KeyValue
}

var _ runtimebootstrap.Store = (*RuntimeStore)(nil)

func NewRuntimeStore(ctx context.Context, url, bucket string) (*RuntimeStore, *problem.Problem) {
	nc, kv, p := openKeyValueBucket(url, bucket, "market-raccoon-dataplane-runtime")
	if p != nil {
		return nil, p
	}
	return &RuntimeStore{nc: nc, kv: kv}, nil
}

func (s *RuntimeStore) Close() error {
	if s == nil || s.nc == nil {
		return nil
	}
	s.nc.Drain()
	s.nc.Close()
	return nil
}

func (s *RuntimeStore) UpsertBinding(_ context.Context, binding dataplane.Binding) *problem.Problem {
	if p := binding.Validate(); p != nil {
		return p
	}
	return s.putJSON(bindingKey(binding.Name), binding)
}

func (s *RuntimeStore) ListBindings(_ context.Context) ([]dataplane.Binding, *problem.Problem) {
	keys, p := s.keysWithPrefix(bindingKeyPrefix)
	if p != nil {
		return nil, p
	}
	out := make([]dataplane.Binding, 0, len(keys))
	for _, key := range keys {
		var binding dataplane.Binding
		if p := s.getJSON(key, &binding); p != nil {
			return nil, p
		}
		out = append(out, binding)
	}
	return out, nil
}

func (s *RuntimeStore) UpsertConfig(_ context.Context, cfg dataplane.Config) *problem.Problem {
	if p := cfg.Validate(); p != nil {
		return p
	}
	return s.putJSON(configKey(cfg.Binding, cfg.Version), cfg)
}

func (s *RuntimeStore) ListConfigs(_ context.Context, binding string) ([]dataplane.Config, *problem.Problem) {
	prefix := configKeyPrefix
	if strings.TrimSpace(binding) != "" {
		prefix = configKeyPrefix + sanitize(binding) + "."
	}
	keys, p := s.keysWithPrefix(prefix)
	if p != nil {
		return nil, p
	}
	out := make([]dataplane.Config, 0, len(keys))
	for _, key := range keys {
		var cfg dataplane.Config
		if p := s.getJSON(key, &cfg); p != nil {
			return nil, p
		}
		out = append(out, cfg)
	}
	return out, nil
}

func (s *RuntimeStore) ActivateConfig(_ context.Context, activation dataplane.Activation) *problem.Problem {
	if p := activation.Validate(); p != nil {
		return p
	}
	return s.putJSON(activationKey(activation.Binding), activation)
}

func (s *RuntimeStore) ActiveConfig(ctx context.Context, binding string) (*dataplane.Config, *problem.Problem) {
	var activation dataplane.Activation
	if p := s.getJSON(activationKey(binding), &activation); p != nil {
		return nil, p
	}
	configs, p := s.ListConfigs(ctx, binding)
	if p != nil {
		return nil, p
	}
	for _, cfg := range configs {
		if cfg.Version == activation.Version {
			return &cfg, nil
		}
	}
	return nil, problem.Newf(problem.NotFound, "active config %q for binding %q not found", activation.Version, binding)
}

func (s *RuntimeStore) putJSON(key string, value any) *problem.Problem {
	return kvPutJSON(s.kv, key, value)
}

func (s *RuntimeStore) getJSON(key string, dst any) *problem.Problem {
	return kvGetJSON(s.kv, key, dst)
}

func (s *RuntimeStore) keysWithPrefix(prefix string) ([]string, *problem.Problem) {
	return kvKeysWithPrefix(s.kv, prefix)
}

func kvPutJSON(kv nats.KeyValue, key string, value any) *problem.Problem {
	payload, err := json.Marshal(value)
	if err != nil {
		return problem.Wrap(err, problem.Internal, "marshal kv value failed")
	}
	if _, err := kv.Put(key, payload); err != nil {
		return problem.Wrap(err, problem.Unavailable, "jetstream kv put failed")
	}
	return nil
}

func kvGetJSON(kv nats.KeyValue, key string, dst any) *problem.Problem {
	entry, err := kv.Get(key)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			return problem.Newf(problem.NotFound, "jetstream kv key %q not found", key)
		}
		return problem.Wrap(err, problem.Unavailable, "jetstream kv get failed")
	}
	if err := json.Unmarshal(entry.Value(), dst); err != nil {
		return problem.Wrap(err, problem.Internal, "jetstream kv decode failed")
	}
	return nil
}

func kvKeysWithPrefix(kv nats.KeyValue, prefix string) ([]string, *problem.Problem) {
	keys, err := kv.Keys()
	if err != nil {
		if errors.Is(err, nats.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, problem.Wrap(err, problem.Unavailable, "jetstream kv keys failed")
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	return out, nil
}

func bindingKey(name string) string {
	return bindingKeyPrefix + sanitize(name)
}

func configKey(binding, version string) string {
	return fmt.Sprintf("%s%s.%s", configKeyPrefix, sanitize(binding), sanitize(version))
}

func activationKey(binding string) string {
	return activationKeyPrefix + sanitize(binding)
}

func sanitize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "/", "_")
	return value
}

func openKeyValueBucket(url, bucket, clientName string) (*nats.Conn, nats.KeyValue, *problem.Problem) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil, problem.New(problem.ValidationFailed, "nats url must not be empty")
	}
	if strings.TrimSpace(bucket) == "" {
		bucket = dataplane.DefaultStateBucket
	}

	nc, err := nats.Connect(url, nats.Name(clientName))
	if err != nil {
		return nil, nil, problem.Wrap(err, problem.Unavailable, "nats connect failed")
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, problem.Wrap(err, problem.Unavailable, "jetstream context failed")
	}
	kv, err := js.KeyValue(bucket)
	if err != nil {
		if !errors.Is(err, nats.ErrBucketNotFound) {
			nc.Close()
			return nil, nil, problem.Wrap(err, problem.Unavailable, "jetstream kv lookup failed")
		}
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:  bucket,
			Storage: nats.FileStorage,
		})
		if err != nil {
			nc.Close()
			return nil, nil, problem.Wrap(err, problem.Unavailable, "jetstream kv create failed")
		}
	}
	return nc, kv, nil
}
