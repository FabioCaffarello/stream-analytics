package timescale

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"marketmonkey/event"
	"marketmonkey/pkg/db"
	"marketmonkey/sql/timescale"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
)

type client struct {
	conn *pgx.Conn
}

func NewClient(conn *pgx.Conn) (db.Client, error) {
	if err := runMigrations(context.Background(), conn.Config().ConnString()); err != nil {
		return nil, err
	}

	return &client{
		conn: conn,
	}, nil
}

func runMigrations(ctx context.Context, connString string) error {
	slog.Info("running timescale migrations")
	d, err := iofs.New(timescale.FS, ".")
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

func (s *client) GetStats(pair *event.Pair, from, to, timeframe int64) ([]*event.Stat, error) {
	return nil, nil
}

func (s *client) GetFirstCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error) {
	query := `
		SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final
		FROM candles
		WHERE exchange = $1 AND symbol = $2
		ORDER BY unix ASC
		LIMIT 1
	`

	var candle event.Candle
	err := s.conn.QueryRow(ctx, query, pair.Exchange, pair.Symbol).Scan(
		&candle.Unix,
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
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get first candle: %w", err)
	}

	return &candle, nil
}

func (s *client) GetLastCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error) {
	query := `
		SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final
		FROM candles
		WHERE exchange = $1 AND symbol = $2
		ORDER BY unix DESC
		LIMIT 1
	`
	var candle event.Candle
	err := s.conn.QueryRow(ctx, query, pair.Exchange, pair.Symbol).Scan(
		&candle.Unix,
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
	return &candle, nil
}

func (s *client) GetAllCandles(pair *event.Pair) ([]*event.Candle, error) {
	query := `
		SELECT unix
		FROM candles
		WHERE exchange = $1 AND symbol = $2
		ORDER BY unix`

	rows, err := s.conn.Query(context.Background(), query, pair.Exchange, pair.Symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candles []*event.Candle
	for rows.Next() {
		var candle event.Candle
		err := rows.Scan(&candle.Unix)
		if err != nil {
			return nil, err
		}
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
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
    ON CONFLICT (unix, exchange, symbol) DO NOTHING`

	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, candle := range candles {
		_, err := tx.Exec(ctx, query,
			candle.Unix,
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

	return tx.Commit(ctx)
}

func (s *client) InsertVolume(ctx context.Context, volume *event.Volume) error {
	const query = `
        INSERT INTO volumes (
            unix, exchange, symbol,
            prices, buys, sells, price_group, final
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (exchange, symbol, unix) DO NOTHING`
	_, err := s.conn.Exec(ctx, query,
		volume.Unix,
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
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, volume := range volumes {
		const query = `
        INSERT INTO volumes (
            unix, exchange, symbol,
            prices, buys, sells, price_group, final
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (exchange, symbol, unix) DO NOTHING`
		_, err := tx.Exec(ctx, query,
			volume.Unix,
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
	return tx.Commit(ctx)
}

func (s *client) InsertHeatmap(ctx context.Context, heatmap *event.Heatmap) error {
	const query = `
        INSERT INTO heatmaps (
            unix, price_group, exchange, symbol,
            prices, sizes, min_price, max_price, max_size
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (exchange, symbol, unix) DO NOTHING
    `
	_, err := s.conn.Exec(ctx, query,
		heatmap.Unix-(heatmap.Unix%60),
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
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        ON CONFLICT (unix, exchange, symbol, timeframe) DO NOTHING
    `
	tx, err := s.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, stat := range stats.Values {
		_, err := tx.Exec(ctx, query,
			stat.Unix,
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
	return tx.Commit(ctx)
}

func (s *client) GetCandles(pair *event.Pair, from, to, timeframe int64) ([]*event.Candle, error) {
	const query = `
       SELECT
            time_bucket($5 * INTERVAL '1 second', to_timestamp(unix)) AS bucket,
            first(open, to_timestamp(unix))  AS open,
            max(high)                       AS high,
            min(low)                        AS low,
            last(close, to_timestamp(unix)) AS close,
            sum(vbuy)   AS vbuy,
            sum(vsell)  AS vsell,
            sum(tbuy)   AS tbuy,
            sum(tsell)  AS tsell,
            bool_and(final) AS final
        FROM candles
        WHERE exchange = $1
        AND symbol   = $2
        AND unix    >= $3
        AND unix    <= $4
        GROUP BY bucket
        ORDER BY bucket
	`
	rows, err := s.conn.Query(context.Background(), query,
		pair.Exchange, pair.Symbol, from, to, timeframe)
	if err != nil {
		return nil, fmt.Errorf("query error: %v", err)
	}
	defer rows.Close()

	var candles []*event.Candle
	for rows.Next() {
		var c event.Candle
		var bucket time.Time
		err := rows.Scan(
			&bucket,
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
		c.Unix = bucket.Unix()
		candles = append(candles, &c)
	}
	return candles, rows.Err()
}

func (s *client) GetFirstVolume(ctx context.Context, pair *event.Pair) (*event.Volume, error) {
	const query = `
		SELECT
			unix, prices, buys, sells, price_group, final
		FROM volumes
		WHERE exchange = $1 AND symbol = $2
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
	const query = `
		WITH time_buckets AS (
		-- Group records into time buckets
		SELECT
				time_bucket($5 * INTERVAL '1 second', to_timestamp(unix)) AS bucket,
				exchange, symbol
		FROM volumes
		WHERE exchange = $1
			AND symbol = $2
			AND unix >= $3
			AND unix <= $4
		GROUP BY bucket, exchange, symbol
    ),
    all_prices AS (
        -- Unnest price arrays to get all unique prices in each bucket
        SELECT
            time_bucket($5 * INTERVAL '1 second', to_timestamp(v.unix)) AS bucket,
            price
        FROM volumes v,
            UNNEST(v.prices) AS price
        WHERE v.exchange = $1
          AND v.symbol = $2
          AND v.unix >= $3
          AND v.unix <= $4
        GROUP BY bucket, price
        ORDER BY bucket, price
    ),
    price_arrays AS (
        -- Reaggregate prices into arrays per bucket
        SELECT
            bucket,
            array_agg(price ORDER BY price) AS prices
        FROM all_prices
        GROUP BY bucket
    ),
    volume_data AS (
        -- Unnest arrays to get (price, buy, sell) for each record
        SELECT
            time_bucket($5 * INTERVAL '1 second', to_timestamp(v.unix)) AS bucket,
            v.prices[i] AS price,
            v.buys[i] AS buy,
            v.sells[i] AS sell
        FROM volumes v,
            generate_subscripts(v.prices, 1) AS i
        WHERE v.exchange = $1
          AND v.symbol = $2
          AND v.unix >= $3
          AND v.unix <= $4
    ),
    volume_sums AS (
        -- Sum buys/sells per price in each bucket
        SELECT
            bucket,
            price,
            SUM(buy) AS total_buy,
            SUM(sell) AS total_sell
        FROM volume_data
        GROUP BY bucket, price
    ),
    buy_arrays AS (
        -- Create ordered buy arrays for each bucket based on price order
        SELECT
            v.bucket,
            array_agg(COALESCE(vs.total_buy, 0) ORDER BY ap.price) AS buys
        FROM time_buckets v
        JOIN all_prices ap ON v.bucket = ap.bucket
        LEFT JOIN volume_sums vs ON v.bucket = vs.bucket AND ap.price = vs.price
        GROUP BY v.bucket
    ),
    sell_arrays AS (
        -- Create ordered sell arrays for each bucket based on price order
        SELECT
            v.bucket,
            array_agg(COALESCE(vs.total_sell, 0) ORDER BY ap.price) AS sells
        FROM time_buckets v
        JOIN all_prices ap ON v.bucket = ap.bucket
        LEFT JOIN volume_sums vs ON v.bucket = vs.bucket AND ap.price = vs.price
        GROUP BY v.bucket
    ),
    final_status AS (
        -- Determine if all records in the bucket are final
        SELECT
            time_bucket($5 * INTERVAL '1 second', to_timestamp(unix)) AS bucket,
            bool_and(final) AS is_final,
            MAX(price_group) AS price_group
        FROM volumes
        WHERE exchange = $1
          AND symbol = $2
          AND unix >= $3
          AND unix <= $4
        GROUP BY bucket
    )
    SELECT
        b.bucket,
        p.prices,
        ba.buys,
        sa.sells,
        f.price_group,
        f.is_final AS final
    FROM time_buckets b
    JOIN price_arrays p ON b.bucket = p.bucket
    JOIN buy_arrays ba ON b.bucket = ba.bucket
    JOIN sell_arrays sa ON b.bucket = sa.bucket
    JOIN final_status f ON b.bucket = f.bucket
    ORDER BY b.bucket;
		`

	rows, err := s.conn.Query(context.Background(), query,
		pair.Exchange,
		pair.Symbol,
		from,
		to,
		timeframe,
	)
	if err != nil {
		return nil, fmt.Errorf("volume query error: %w", err)
	}
	defer rows.Close()

	var volumes []*event.Volume
	for rows.Next() {
		var v event.Volume
		var bucket time.Time
		if err := rows.Scan(
			&bucket,
			&v.Prices,
			&v.Buys,
			&v.Sells,
			&v.PriceGroup,
			&v.Final,
		); err != nil {
			return nil, fmt.Errorf("volume scan error: %w", err)
		}
		v.Unix = bucket.Unix()
		v.Pair = pair
		volumes = append(volumes, &v)
	}

	return volumes, rows.Err()
}

func (s *client) GetHeatmaps(pair *event.Pair, from, to, timeframe int64) ([]*event.Heatmap, error) {
	const query = `
        SELECT
            time_bucket($5 * INTERVAL '1 second', to_timestamp(unix)) AS bucket,
            LAST(prices, to_timestamp(unix)) AS prices,
            LAST(sizes, to_timestamp(unix)) AS sizes,
            LAST(price_group, to_timestamp(unix)) AS price_group,
            LAST(min_price, to_timestamp(unix)) AS min_price,
            LAST(max_price, to_timestamp(unix)) AS max_price,
            LAST(max_size, to_timestamp(unix)) AS max_size
        FROM heatmaps
        WHERE exchange = $1
          AND symbol = $2
          AND unix >= $3
          AND unix <= $4
        GROUP BY bucket
        ORDER BY bucket`

	rows, err := s.conn.Query(context.Background(), query,
		pair.Exchange,
		pair.Symbol,
		from,
		to,
		timeframe,
	)
	if err != nil {
		return nil, fmt.Errorf("heatmap query error: %w", err)
	}
	defer rows.Close()

	var heatmaps []*event.Heatmap
	for rows.Next() {
		var hm event.Heatmap
		var bucket time.Time
		if err := rows.Scan(
			&bucket,
			&hm.Prices,
			&hm.Sizes,
			&hm.PriceGroup,
			&hm.MinPrice,
			&hm.MaxPrice,
			&hm.MaxSize,
		); err != nil {
			return nil, fmt.Errorf("heatmap scan error: %w", err)
		}
		hm.Unix = bucket.Unix()
		hm.Pair = pair
		heatmaps = append(heatmaps, &hm)
	}

	return heatmaps, rows.Err()
}
