package shardregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type jsKV struct {
	kv nats.KeyValue
}

func (j *jsKV) Get(key string) (kvEntry, error) {
	entry, err := j.kv.Get(key)
	if err != nil {
		return nil, normalizeKVError(err)
	}
	return entry, nil
}

func (j *jsKV) Create(key string, value []byte) (uint64, error) {
	rev, err := j.kv.Create(key, value)
	if err != nil {
		return 0, normalizeKVError(err)
	}
	return rev, nil
}

func (j *jsKV) Update(key string, value []byte, revision uint64) (uint64, error) {
	rev, err := j.kv.Update(key, value, revision)
	if err != nil {
		return 0, normalizeKVError(err)
	}
	return rev, nil
}

func (j *jsKV) Delete(key string) error {
	err := j.kv.Delete(key)
	if err != nil {
		return normalizeKVError(err)
	}
	return nil
}

func (j *jsKV) Keys() ([]string, error) {
	keys, err := j.kv.Keys()
	if err != nil {
		return nil, normalizeKVError(err)
	}
	return keys, nil
}

func NewJetStreamKV(ctx context.Context, url, bucket string, cfg Config) (*Registry, *nats.Conn, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, nil, fmt.Errorf("shardregistry: nats url must not be empty")
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		bucket = DefaultBucket
	}

	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = DefaultLeaseTTL
	}
	heartbeat := cfg.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeatInterval
	}

	nc, err := nats.Connect(url, nats.Name("market-raccoon-shard-registry"))
	if err != nil {
		return nil, nil, err
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, err
	}

	kv, err := js.KeyValue(bucket)
	if err != nil {
		if !errors.Is(err, nats.ErrBucketNotFound) {
			nc.Close()
			return nil, nil, err
		}
		kv, err = js.CreateKeyValue(&nats.KeyValueConfig{
			Bucket:  bucket,
			TTL:     leaseTTL,
			Storage: nats.FileStorage,
		})
		if err != nil {
			nc.Close()
			return nil, nil, err
		}
	}

	registry := NewWithKV(&jsKV{kv: kv}, Config{
		LeaseTTL:          leaseTTL,
		HeartbeatInterval: heartbeat,
	})
	registry.now = func() time.Time {
		select {
		case <-ctx.Done():
			return time.Now().UTC()
		default:
			return time.Now().UTC()
		}
	}
	return registry, nc, nil
}

func normalizeKVError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, nats.ErrKeyNotFound) {
		return errKeyNotFound
	}
	if errors.Is(err, nats.ErrKeyExists) {
		return errKeyExists
	}
	if strings.Contains(strings.ToLower(err.Error()), "wrong last sequence") {
		return errWrongVersion
	}
	return err
}
