package clickhouse

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	adapterstorage "github.com/FabioCaffarello/stream-analytics/internal/adapters/storage"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// PoolConfig controls ClickHouse connection lifecycle.
type PoolConfig struct {
	Addrs           []string
	Database        string
	Username        string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
}

// Pool wraps a clickhouse.Conn.
type Pool struct {
	conn clickhouse.Conn
}

func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, *problem.Problem) {
	if len(cfg.Addrs) == 0 {
		return nil, problem.New(problem.InvalidArgument, "clickhouse addrs must not be empty")
	}
	if cfg.Database == "" {
		return nil, problem.New(problem.InvalidArgument, "clickhouse database must not be empty")
	}

	opts := &clickhouse.Options{
		Addr: cfg.Addrs,
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse open failed")
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse ping failed")
	}
	return &Pool{conn: conn}, nil
}

func (p *Pool) Close() *problem.Problem {
	if p == nil || p.conn == nil {
		return nil
	}
	if err := p.conn.Close(); err != nil {
		return problem.Wrap(err, problem.Unavailable, "clickhouse close failed")
	}
	return nil
}

func (p *Pool) Conn() clickhouse.Conn {
	if p == nil {
		return nil
	}
	return p.conn
}

func (p *Pool) Healthy(ctx context.Context) *problem.Problem {
	if p == nil || p.conn == nil {
		return problem.New(problem.ValidationFailed, "clickhouse pool is nil")
	}
	if err := p.conn.Ping(ctx); err != nil {
		return problem.Wrap(err, problem.Unavailable, "clickhouse health check failed")
	}
	return nil
}

func (p *Pool) PrepareInsert(ctx context.Context, query string) (adapterstorage.BatchInserter, *problem.Problem) {
	if p == nil || p.conn == nil {
		return nil, problem.New(problem.ValidationFailed, "clickhouse pool is nil")
	}
	batch, err := p.conn.PrepareBatch(ctx, query)
	if err != nil {
		return nil, problem.Wrap(err, problem.Unavailable, "clickhouse prepare batch failed")
	}
	return &batchInserter{batch: batch}, nil
}

type batchInserter struct {
	batch driver.Batch
	rows  int64
}

func (b *batchInserter) AppendRow(_ context.Context, values ...any) *problem.Problem {
	if b == nil || b.batch == nil {
		return problem.New(problem.ValidationFailed, "clickhouse batch is nil")
	}
	if err := b.batch.Append(values...); err != nil {
		return problem.Wrap(err, problem.Unavailable, "clickhouse append failed")
	}
	b.rows++
	return nil
}

func (b *batchInserter) Flush(_ context.Context) (int64, *problem.Problem) {
	if b == nil || b.batch == nil {
		return 0, problem.New(problem.ValidationFailed, "clickhouse batch is nil")
	}
	if err := b.batch.Send(); err != nil {
		return 0, problem.Wrap(err, problem.Unavailable, "clickhouse batch send failed")
	}
	return b.rows, nil
}

func (b *batchInserter) Close() *problem.Problem {
	if b == nil || b.batch == nil {
		return nil
	}
	if err := b.batch.Abort(); err != nil {
		return problem.Wrap(err, problem.Unavailable, "clickhouse batch abort failed")
	}
	return nil
}
