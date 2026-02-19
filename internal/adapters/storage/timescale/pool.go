package timescale

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/shared/problem"
)

// PoolConfig controls Timescale pgxpool behavior.
type PoolConfig struct {
	DSN               string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
}

// Pool wraps pgxpool for adapter-level health checks and test seams.
type Pool struct {
	pool *pgxpool.Pool
}

var _ adapterstorage.SQLExecutor = (*Pool)(nil)

func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, *problem.Problem) {
	if cfg.DSN == "" {
		return nil, problem.New(problem.InvalidArgument, "timescale DSN must not be empty")
	}

	parsed, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, problem.Wrap(err, problem.InvalidArgument, "invalid timescale DSN")
	}
	if cfg.MaxConns > 0 {
		parsed.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		parsed.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		parsed.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		parsed.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		parsed.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "failed to create timescale pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, problem.Wrap(err, problem.Unavailable, "timescale ping failed")
	}

	return &Pool{pool: pool}, nil
}

func (p *Pool) Close() {
	if p == nil || p.pool == nil {
		return
	}
	p.pool.Close()
}

func (p *Pool) Raw() *pgxpool.Pool {
	if p == nil {
		return nil
	}
	return p.pool
}

func (p *Pool) Healthy(ctx context.Context) *problem.Problem {
	if p == nil || p.pool == nil {
		return problem.New(problem.ValidationFailed, "timescale pool is nil")
	}
	if err := p.pool.Ping(ctx); err != nil {
		return problem.Wrap(err, problem.Unavailable, "timescale health check failed")
	}
	return nil
}

func (p *Pool) Exec(ctx context.Context, query string, args ...any) (int64, *problem.Problem) {
	if p == nil || p.pool == nil {
		return 0, problem.New(problem.ValidationFailed, "timescale pool is nil")
	}
	tag, err := p.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, problem.Wrap(err, problem.Unavailable, "timescale exec failed")
	}
	return tag.RowsAffected(), nil
}

func (p *Pool) QueryRow(ctx context.Context, query string, args ...any) adapterstorage.Row {
	if p == nil || p.pool == nil {
		return nil
	}
	return p.pool.QueryRow(ctx, query, args...)
}
