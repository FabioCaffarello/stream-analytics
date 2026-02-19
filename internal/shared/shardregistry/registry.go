package shardregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/market-raccoon/internal/shared/metrics"
)

const (
	DefaultBucket            = "MR_SHARD_REGISTRY"
	DefaultLeaseTTL          = 30 * time.Second
	DefaultHeartbeatInterval = 10 * time.Second
	DefaultTopologyGrace     = 60 * time.Second
	topologyPollInterval     = time.Second
)

var (
	errKeyNotFound  = errors.New("shardregistry: key not found")
	errKeyExists    = errors.New("shardregistry: key exists")
	errWrongVersion = errors.New("shardregistry: wrong revision")
)

type kvEntry interface {
	Value() []byte
	Revision() uint64
}

type kvStore interface {
	Get(key string) (kvEntry, error)
	Create(key string, value []byte) (uint64, error)
	Update(key string, value []byte, revision uint64) (uint64, error)
	Delete(key string) error
	Keys() ([]string, error)
}

type ownerRecord struct {
	InstanceID        string `json:"instance_id"`
	Hostname          string `json:"hostname"`
	PID               int    `json:"pid"`
	StartedAt         string `json:"started_at"`
	LastHeartbeatUnix int64  `json:"last_heartbeat_unix"`
}

type Config struct {
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
}

type Registry struct {
	kv                kvStore
	leaseTTL          time.Duration
	heartbeatInterval time.Duration
	now               func() time.Time
}

type Lease interface {
	Heartbeat(ctx context.Context) error
	StartHeartbeat(ctx context.Context, onLeaseLost func(error))
	Release(ctx context.Context) error
	LastHeartbeatUnix() int64
}

type lease struct {
	registry   *Registry
	key        string
	instanceID string
	startedAt  string
	hostname   string
	pid        int
	shardIndex int

	rev               atomic.Uint64
	lastHeartbeatUnix atomic.Int64
}

type DualOwnerError struct {
	ShardIndex int
	Current    ownerRecord
}

func (e *DualOwnerError) Error() string {
	return fmt.Sprintf(
		"shard %d already owned by instance_id=%s hostname=%s pid=%d last_heartbeat_unix=%d",
		e.ShardIndex,
		e.Current.InstanceID,
		e.Current.Hostname,
		e.Current.PID,
		e.Current.LastHeartbeatUnix,
	)
}

func NewWithKV(kv kvStore, cfg Config) *Registry {
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = DefaultLeaseTTL
	}
	heartbeat := cfg.HeartbeatInterval
	if heartbeat <= 0 {
		heartbeat = DefaultHeartbeatInterval
	}
	return &Registry{
		kv:                kv,
		leaseTTL:          leaseTTL,
		heartbeatInterval: heartbeat,
		now:               time.Now,
	}
}

//nolint:gocyclo // acquire path intentionally handles CAS, takeover and retry branches.
func (r *Registry) Acquire(ctx context.Context, shardIndex, shardCount int, instanceID string) (Lease, error) {
	if r == nil || r.kv == nil {
		return nil, fmt.Errorf("shardregistry: nil registry")
	}
	if shardCount < 1 {
		return nil, fmt.Errorf("shardregistry: shard_count must be >= 1, got %d", shardCount)
	}
	if shardIndex < 0 || shardIndex >= shardCount {
		return nil, fmt.Errorf("shardregistry: shard_index must be in [0,%d), got %d", shardCount, shardIndex)
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("shardregistry: instance_id must not be empty")
	}

	hostname, _ := os.Hostname()
	key := shardKey(shardIndex)
	startedAt := r.now().UTC().Format(time.RFC3339Nano)
	pid := os.Getpid()

	for i := 0; i < 32; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		entry, err := r.kv.Get(key)
		if err != nil {
			if errors.Is(err, errKeyNotFound) {
				rec := ownerRecord{
					InstanceID:        instanceID,
					Hostname:          hostname,
					PID:               pid,
					StartedAt:         startedAt,
					LastHeartbeatUnix: r.now().Unix(),
				}
				payload, marshalErr := json.Marshal(rec)
				if marshalErr != nil {
					return nil, marshalErr
				}
				rev, createErr := r.kv.Create(key, payload)
				if createErr != nil {
					if errors.Is(createErr, errKeyExists) {
						continue
					}
					return nil, createErr
				}
				l := &lease{
					registry:   r,
					key:        key,
					instanceID: instanceID,
					startedAt:  startedAt,
					hostname:   hostname,
					pid:        pid,
					shardIndex: shardIndex,
				}
				l.rev.Store(rev)
				l.lastHeartbeatUnix.Store(rec.LastHeartbeatUnix)
				metrics.SetShardLeaseAgeSeconds(0)
				return l, nil
			}
			return nil, err
		}

		current, decodeErr := decodeOwner(entry.Value())
		if decodeErr != nil {
			return nil, fmt.Errorf("shardregistry: decode existing owner for %s: %w", key, decodeErr)
		}

		nowUnix := r.now().Unix()
		expired := nowUnix-current.LastHeartbeatUnix > int64(r.leaseTTL.Seconds())
		if current.InstanceID != instanceID && !expired {
			metrics.IncShardOwnerConflicts()
			return nil, &DualOwnerError{ShardIndex: shardIndex, Current: current}
		}

		rec := ownerRecord{
			InstanceID:        instanceID,
			Hostname:          hostname,
			PID:               pid,
			StartedAt:         startedAt,
			LastHeartbeatUnix: nowUnix,
		}
		payload, marshalErr := json.Marshal(rec)
		if marshalErr != nil {
			return nil, marshalErr
		}
		rev, updateErr := r.kv.Update(key, payload, entry.Revision())
		if updateErr != nil {
			if errors.Is(updateErr, errWrongVersion) || errors.Is(updateErr, errKeyNotFound) {
				continue
			}
			return nil, updateErr
		}

		l := &lease{
			registry:   r,
			key:        key,
			instanceID: instanceID,
			startedAt:  startedAt,
			hostname:   hostname,
			pid:        pid,
			shardIndex: shardIndex,
		}
		l.rev.Store(rev)
		l.lastHeartbeatUnix.Store(rec.LastHeartbeatUnix)
		metrics.SetShardLeaseAgeSeconds(0)
		return l, nil
	}

	return nil, fmt.Errorf("shardregistry: acquire retry limit reached for shard %d", shardIndex)
}

func (r *Registry) TopologyComplete(ctx context.Context, shardCount int) (bool, error) {
	if shardCount < 1 {
		return false, fmt.Errorf("shardregistry: shard_count must be >= 1, got %d", shardCount)
	}
	keys, err := r.kv.Keys()
	if err != nil {
		return false, err
	}

	nowUnix := r.now().Unix()
	seen := make(map[int]struct{}, shardCount)
	for _, key := range keys {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		idx, ok := parseShardIndexFromKey(key)
		if !ok || idx < 0 || idx >= shardCount {
			continue
		}
		entry, getErr := r.kv.Get(key)
		if getErr != nil {
			if errors.Is(getErr, errKeyNotFound) {
				continue
			}
			return false, getErr
		}
		rec, decodeErr := decodeOwner(entry.Value())
		if decodeErr != nil {
			continue
		}
		if nowUnix-rec.LastHeartbeatUnix > int64(r.leaseTTL.Seconds()) {
			continue
		}
		seen[idx] = struct{}{}
	}
	return len(seen) == shardCount, nil
}

func (r *Registry) WaitForTopology(ctx context.Context, shardCount int, grace time.Duration) (bool, error) {
	if grace <= 0 {
		grace = DefaultTopologyGrace
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()

	ticker := time.NewTicker(topologyPollInterval)
	defer ticker.Stop()

	for {
		complete, err := r.TopologyComplete(deadlineCtx, shardCount)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return false, nil
			}
			return false, err
		}
		if complete {
			return true, nil
		}
		select {
		case <-deadlineCtx.Done():
			return false, nil
		case <-ticker.C:
		}
	}
}

func (l *lease) Heartbeat(ctx context.Context) error {
	if l == nil || l.registry == nil || l.registry.kv == nil {
		return fmt.Errorf("shardregistry: nil lease")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	entry, err := l.registry.kv.Get(l.key)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return fmt.Errorf("lease lost for %s: key missing", l.key)
		}
		return err
	}

	current, decodeErr := decodeOwner(entry.Value())
	if decodeErr != nil {
		return decodeErr
	}
	if current.InstanceID != l.instanceID {
		metrics.IncShardOwnerConflicts()
		return fmt.Errorf("lease lost for %s: owner changed to %s", l.key, current.InstanceID)
	}

	nowUnix := l.registry.now().Unix()
	rec := ownerRecord{
		InstanceID:        l.instanceID,
		Hostname:          l.hostname,
		PID:               l.pid,
		StartedAt:         l.startedAt,
		LastHeartbeatUnix: nowUnix,
	}
	payload, marshalErr := json.Marshal(rec)
	if marshalErr != nil {
		return marshalErr
	}

	rev, updateErr := l.registry.kv.Update(l.key, payload, entry.Revision())
	if updateErr != nil {
		if errors.Is(updateErr, errWrongVersion) || errors.Is(updateErr, errKeyNotFound) {
			return fmt.Errorf("lease lost for %s: %w", l.key, updateErr)
		}
		return updateErr
	}
	l.rev.Store(rev)
	l.lastHeartbeatUnix.Store(nowUnix)
	metrics.SetShardLeaseAgeSeconds(0)
	return nil
}

func (l *lease) StartHeartbeat(ctx context.Context, onLeaseLost func(error)) {
	if l == nil || l.registry == nil {
		return
	}
	interval := l.registry.heartbeatInterval
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				last := l.LastHeartbeatUnix()
				if last > 0 {
					metrics.SetShardLeaseAgeSeconds(float64(l.registry.now().Unix() - last))
				}
				err := l.Heartbeat(ctx)
				if err != nil {
					if onLeaseLost != nil {
						onLeaseLost(err)
					}
					return
				}
			}
		}
	}()
}

func (l *lease) Release(ctx context.Context) error {
	if l == nil || l.registry == nil || l.registry.kv == nil {
		return nil
	}
	entry, err := l.registry.kv.Get(l.key)
	if err != nil {
		if errors.Is(err, errKeyNotFound) {
			return nil
		}
		return err
	}

	current, decodeErr := decodeOwner(entry.Value())
	if decodeErr != nil {
		return nil
	}
	if current.InstanceID != l.instanceID {
		slog.Warn("shard lease release skipped: owner changed",
			"key", l.key,
			"instance_id", l.instanceID,
			"owner_instance_id", current.InstanceID,
		)
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return l.registry.kv.Delete(l.key)
}

func (l *lease) LastHeartbeatUnix() int64 {
	if l == nil {
		return 0
	}
	return l.lastHeartbeatUnix.Load()
}

func shardKey(shardIndex int) string {
	return "shard/" + strconv.Itoa(shardIndex)
}

func parseShardIndexFromKey(key string) (int, bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "shard/") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(key, "shard/"))
	if err != nil {
		return 0, false
	}
	return n, true
}

func decodeOwner(raw []byte) (ownerRecord, error) {
	var rec ownerRecord
	if len(raw) == 0 {
		return rec, fmt.Errorf("empty owner record")
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return ownerRecord{}, err
	}
	rec.InstanceID = strings.TrimSpace(rec.InstanceID)
	rec.Hostname = strings.TrimSpace(rec.Hostname)
	rec.StartedAt = strings.TrimSpace(rec.StartedAt)
	if rec.InstanceID == "" {
		return ownerRecord{}, fmt.Errorf("owner record missing instance_id")
	}
	return rec, nil
}
