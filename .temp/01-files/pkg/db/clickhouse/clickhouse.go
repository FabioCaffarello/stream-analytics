package clickhouse

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"marketmonkey/event"
	"marketmonkey/pkg/db"
	clickhouseFiles "marketmonkey/sql/clickhouse"

	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/clickhouse"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

type client struct {
	conn driver.Conn
}

func NewClient(addr string, dbName string, username string, password string) (db.Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: dbName,
			Username: username,
			Password: password,
		},
		Debug: false,
		Debugf: func(format string, v ...any) {
			fmt.Printf(format+"\n", v...)
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		DialTimeout:          time.Second * 30,
		MaxOpenConns:         5,
		MaxIdleConns:         5,
		ConnMaxLifetime:      time.Duration(10) * time.Minute,
		ConnOpenStrategy:     clickhouse.ConnOpenInOrder,
		BlockBufferSize:      10,
		MaxCompressionBuffer: 10240,
	})
	if err != nil {
		return nil, err
	}

	connString := fmt.Sprintf("clickhouse://%s:%s@%s/%s",
		username,
		password,
		addr,
		dbName,
	)

	if err := runMigrations(context.Background(), connString); err != nil {
		return nil, err
	}
	return &client{
		conn: conn,
	}, nil
}

func runMigrations(_ context.Context, connString string) error {
	slog.Info("running clickhouse migrations")
	d, err := iofs.New(clickhouseFiles.FS, ".")
	if err != nil {
		log.Fatal(err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, connString)
	if err != nil {
		log.Fatal(err)
	}
	err = m.Up()
	if err != nil {
		if err == migrate.ErrNoChange {
			slog.Info("Database schema is up to date")
		} else {
			slog.Error("failed to run database migrations", "err", err)
			os.Exit(1)
		}
	} else {
		slog.Info("Updated database schema")
	}
	m.Close()
	return nil
}

func (s *client) GetFirstCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error) {
	query := `
		SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final
		FROM candles
		WHERE exchange = ? AND symbol = ?
		ORDER BY unix ASC
		LIMIT 1
	`

	var candle event.Candle
	var ts time.Time
	err := s.conn.QueryRow(ctx, query, pair.Exchange, pair.Symbol).Scan(
		&ts,
		&candle.Open,
		&candle.Close,
		&candle.High,
		&candle.Low,
		&candle.Vbuy,
		&candle.Vsell,
		&candle.Tbuy,
		&candle.Tsell,
		&candle.Final,
	)

	if err != nil {
		return nil, err
	}
	candle.Unix = ts.Unix()
	return &candle, nil
}

func (s *client) GetLastCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error) {
	query := `
		SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final
		FROM candles
		WHERE exchange = ? AND symbol = ?
		ORDER BY unix DESC
		LIMIT 1
	`

	var candle event.Candle
	var ts time.Time
	err := s.conn.QueryRow(ctx, query, pair.Exchange, pair.Symbol).Scan(
		&ts,
		&candle.Open,
		&candle.Close,
		&candle.High,
		&candle.Low,
		&candle.Vbuy,
		&candle.Vsell,
		&candle.Tbuy,
		&candle.Tsell,
		&candle.Final,
	)

	if err != nil {
		return nil, err
	}
	candle.Unix = ts.Unix()
	return &candle, nil
}

func (s *client) GetAllCandles(pair *event.Pair) ([]*event.Candle, error) {
	query := `
		SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final
		FROM candles
		WHERE exchange = ? AND symbol = ?
		ORDER BY unix`

	rows, err := s.conn.Query(context.Background(), query, pair.Exchange, pair.Symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candles []*event.Candle
	for rows.Next() {
		var candle event.Candle
		var ts time.Time
		err := rows.Scan(&ts,
			&candle.Open,
			&candle.Close,
			&candle.High,
			&candle.Low,
			&candle.Vbuy,
			&candle.Vsell,
			&candle.Tbuy,
			&candle.Tsell,
			&candle.Final,
		)
		if err != nil {
			return nil, err
		}
		candle.Unix = ts.Unix()
		candles = append(candles, &candle)
	}
	return candles, rows.Err()
}

func (s *client) InsertCandles(ctx context.Context, pair *event.Pair, candles []*event.Candle) error {
	if len(candles) == 0 {
		return nil
	}

	const query = `
    INSERT INTO candles (unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final, exchange, symbol)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	defer batch.Send()

	for _, candle := range candles {
		err := batch.Append(
			time.Unix(candle.Unix, 0),
			candle.Open,
			candle.Close,
			candle.High,
			candle.Low,
			candle.Vbuy,
			candle.Vsell,
			candle.Tbuy,
			candle.Tsell,
			candle.Final,
			pair.Exchange,
			pair.Symbol,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *client) InsertVolume(ctx context.Context, volume *event.Volume) error {
	const query = `
        INSERT INTO volumes (
            unix, exchange, symbol,
            prices, buys, sells, price_group, final
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	defer batch.Send()

	err = batch.Append(
		time.Unix(volume.Unix, 0),
		volume.Pair.Exchange,
		volume.Pair.Symbol,
		volume.Prices,
		volume.Buys,
		volume.Sells,
		volume.PriceGroup,
		volume.Final,
	)
	return err
}

func (s *client) InsertVolumes(ctx context.Context, volumes []*event.Volume) error {
	const query = `
        INSERT INTO volumes (
            unix, exchange, symbol,
            prices, buys, sells, price_group, final
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	defer batch.Send()

	for _, volume := range volumes {
		err := batch.Append(
			time.Unix(volume.Unix, 0),
			volume.Pair.Exchange,
			volume.Pair.Symbol,
			volume.Prices,
			volume.Buys,
			volume.Sells,
			volume.PriceGroup,
			volume.Final,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *client) InsertHeatmap(ctx context.Context, heatmap *event.Heatmap) error {
	const query = `
        INSERT INTO heatmaps (
            unix, price_group, exchange, symbol,
            prices, sizes, min_price, max_price, max_size
        )
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	defer batch.Send()

	err = batch.Append(
		time.Unix(heatmap.Unix-(heatmap.Unix%60), 0),
		heatmap.PriceGroup,
		heatmap.Pair.Exchange,
		heatmap.Pair.Symbol,
		heatmap.Prices,
		heatmap.Sizes,
		heatmap.MinPrice,
		heatmap.MaxPrice,
		heatmap.MaxSize,
	)
	return err
}

func (s *client) InsertStats(ctx context.Context, stats *event.Stats) error {
	if len(stats.Values) == 0 {
		return nil
	}

	const query = `
        INSERT INTO stats (
            unix, exchange, symbol, timeframe,
            liq_vsell, liq_vbuy, mark_price, funding,
            tbuy, tsell, final
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	batch, err := s.conn.PrepareBatch(ctx, query)
	if err != nil {
		return err
	}
	defer batch.Send()

	for _, stat := range stats.Values {
		err := batch.Append(
			time.Unix(stat.Unix, 0),
			stats.Pair.Exchange,
			stats.Pair.Symbol,
			stats.Timeframe,
			stat.LiqVsell,
			stat.LiqVbuy,
			stat.MarkPrice,
			stat.Funding,
			stat.Tbuy,
			stat.Tsell,
			stat.Final,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *client) GetStats(pair *event.Pair, from, to, timeframe int64) ([]*event.Stat, error) {
	const query = `
        SELECT
            toStartOfInterval(toDateTime(unix), INTERVAL ? SECOND) AS bucket,
            sum(liq_vsell) AS liqVsell,
            sum(liq_vbuy) AS liqVbuy,
            argMax(mark_price, toDateTime(unix)) AS markPrice,
            argMax(funding, toDateTime(unix)) AS funding,
            sum(tbuy) AS tbuy,
            sum(tsell) AS tsell,
            min(final) AS final
        FROM stats
        WHERE exchange = ?
          AND symbol = ?
          AND unix >= ?
          AND unix <= ?
        GROUP BY bucket
        ORDER BY bucket
    `

	rows, err := s.conn.Query(context.Background(), query,
		timeframe,
		pair.Exchange,
		pair.Symbol,
		from,
		to,
	)
	if err != nil {
		return nil, fmt.Errorf("query error: %v", err)
	}
	defer rows.Close()

	var stats []*event.Stat
	for rows.Next() {
		var stat event.Stat
		var ts time.Time
		err := rows.Scan(
			&ts,
			&stat.LiqVsell, // liq_v_sell from DB mapped to LiqVsell
			&stat.LiqVbuy,  // liq_v_buy from DB mapped to LiqVbuy
			&stat.MarkPrice,
			&stat.Funding,
			&stat.Tbuy,
			&stat.Tsell,
			&stat.Final,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error: %v", err)
		}
		stat.Unix = ts.Unix()
		stats = append(stats, &stat)
	}
	return stats, rows.Err()
}

func (s *client) GetCandles(pair *event.Pair, from, to, timeframe int64) ([]*event.Candle, error) {
	const query = `
        SELECT
            toStartOfInterval(toDateTime(unix), INTERVAL ? SECOND) AS bucket,
            argMin(open, toDateTime(unix)) AS open,
            max(high) AS high,
            min(low) AS low,
            argMax(close, toDateTime(unix)) AS close,
            sum(vbuy) AS vbuy,
            sum(vsell) AS vsell,
            sum(tbuy) AS tbuy,
            sum(tsell) AS tsell,
            min(final) AS final
        FROM candles
        WHERE exchange = ?
        AND symbol = ?
        AND unix >= ?
        AND unix <= ?
        GROUP BY bucket
        ORDER BY bucket`

	rows, err := s.conn.Query(context.Background(), query,
		timeframe,
		pair.Exchange,
		pair.Symbol,
		from,
		to,
	)
	if err != nil {
		return nil, fmt.Errorf("query error: %v", err)
	}
	defer rows.Close()

	var candles []*event.Candle
	for rows.Next() {
		var c event.Candle
		var ts time.Time
		err := rows.Scan(
			&ts,
			&c.Open,
			&c.High,
			&c.Low,
			&c.Close,
			&c.Vbuy,
			&c.Vsell,
			&c.Tbuy,
			&c.Tsell,
			&c.Final,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error: %v", err)
		}
		c.Unix = ts.Unix()
		candles = append(candles, &c)
	}
	return candles, rows.Err()
}

func (s *client) GetFirstVolume(ctx context.Context, pair *event.Pair) (*event.Volume, error) {
	const query = `
		SELECT
			unix, prices, buys, sells, price_group, final
		FROM volumes
		WHERE exchange = ? AND symbol = ?
		ORDER BY unix ASC
		LIMIT 1`

	var volume event.Volume
	var ts time.Time
	err := s.conn.QueryRow(ctx, query, pair.Exchange, pair.Symbol).Scan(
		&ts,
		&volume.Prices,
		&volume.Buys,
		&volume.Sells,
		&volume.PriceGroup,
		&volume.Final,
	)
	if err != nil {
		return nil, err
	}
	volume.Unix = ts.Unix()
	volume.Pair = pair
	return &volume, nil
}

func (s *client) GetVolumes(pair *event.Pair, from, to, timeframe int64) ([]*event.Volume, error) {
	const query = `SELECT
    time_group,
    groupArray(price) AS prices,
    groupArray(total_buy) AS buys,
    groupArray(total_sell) AS sells,
		groupBitAnd(final) AS final,
    max(price_group) AS price_group
FROM (
    SELECT
        toStartOfInterval(unix, INTERVAL $5 SECOND) AS time_group,
        exchange,
        symbol,
        price,
        sum(buy) AS total_buy,
        sum(sell) AS total_sell,
				final,
        price_group
    FROM volumes
    ARRAY JOIN
        prices AS price,
        buys AS buy,
        sells AS sell
			WHERE
				exchange = $1
        AND symbol = $2
        AND unix >= $3
        AND unix <= $4
    GROUP BY
        time_group,
        exchange,
        symbol,
        price,
				final,
				price_group
    ORDER BY price ASC
)
GROUP BY
    time_group,
    exchange,
    symbol
ORDER BY
    time_group ASC,
    exchange,
    symbol`
	rows, err := s.conn.Query(context.Background(), query,
		pair.Exchange,
		pair.Symbol,
		time.Unix(from, 0),
		time.Unix(to, 0),
		timeframe,
	)
	if err != nil {
		return nil, fmt.Errorf("volume query error: %w", err)
	}
	defer rows.Close()

	var volumes []*event.Volume
	for rows.Next() {
		var v event.Volume
		var ts time.Time
		if err := rows.Scan(
			&ts,
			&v.Prices,
			&v.Buys,
			&v.Sells,
			&v.Final,
			&v.PriceGroup,
		); err != nil {
			return nil, fmt.Errorf("volume scan error: %w", err)
		}
		v.Pair = pair
		v.Unix = ts.Unix()
		volumes = append(volumes, &v)
	}

	return volumes, rows.Err()
}

func (s *client) GetHeatmaps(pair *event.Pair, from, to, timeframe int64) ([]*event.Heatmap, error) {
	const query = `
        SELECT
            toStartOfInterval(toDateTime(unix), INTERVAL ? SECOND) AS ts,
            argMax(prices, toDateTime(unix)) AS prices,
            argMax(sizes, toDateTime(unix)) AS sizes,
            argMax(price_group, toDateTime(unix)) AS price_group,
            argMin(min_price, toDateTime(unix)) AS min_price,
            argMax(max_price, toDateTime(unix)) AS max_price,
            argMax(max_size, toDateTime(unix)) AS max_size
        FROM heatmaps
        WHERE exchange = ?
          AND symbol = ?
          AND unix >= ?
          AND unix <= ?
        GROUP BY ts
        ORDER BY ts`

	rows, err := s.conn.Query(context.Background(), query,
		timeframe,
		pair.Exchange,
		pair.Symbol,
		from,
		to,
	)
	if err != nil {
		return nil, fmt.Errorf("heatmap query error: %w", err)
	}
	defer rows.Close()

	var heatmaps []*event.Heatmap
	for rows.Next() {
		var hm event.Heatmap
		var ts time.Time
		if err := rows.Scan(
			&ts,
			&hm.Prices,
			&hm.Sizes,
			&hm.PriceGroup,
			&hm.MinPrice,
			&hm.MaxPrice,
			&hm.MaxSize,
		); err != nil {
			return nil, fmt.Errorf("heatmap scan error: %w", err)
		}
		hm.Unix = ts.Unix()
		hm.Pair = pair
		heatmaps = append(heatmaps, &hm)
	}

	return heatmaps, rows.Err()
}
