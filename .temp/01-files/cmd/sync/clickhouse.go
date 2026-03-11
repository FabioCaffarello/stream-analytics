package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// chunkSize defines the window size for range queries.
const chunkSize = time.Hour

func main() {
	// Source DB settings
	srcAddr := "source-host:9000"
	srcDB := "marketmonkey"
	srcUser := "default"
	srcPass := ""

	// Target DB settings
	tgtAddr := "target-host:9000"
	tgtDB := "marketmonkey"
	tgtUser := "default"
	tgtPass := ""

	srcConn, err := connect(srcAddr, srcDB, srcUser, srcPass)
	if err != nil {
		log.Fatalf("could not connect to source: %v", err)
	}
	tgtConn, err := connect(tgtAddr, tgtDB, tgtUser, tgtPass)
	if err != nil {
		log.Fatalf("could not connect to target: %v", err)
	}

	ctx := context.Background()

	if err := syncCandles(ctx, srcConn, tgtConn); err != nil {
		log.Fatalf("syncCandles error: %v", err)
	}
	if err := syncVolumes(ctx, srcConn, tgtConn); err != nil {
		log.Fatalf("syncVolumes error: %v", err)
	}
	if err := syncHeatmaps(ctx, srcConn, tgtConn); err != nil {
		log.Fatalf("syncHeatmaps error: %v", err)
	}
	if err := syncStats(ctx, srcConn, tgtConn); err != nil {
		log.Fatalf("syncStats error: %v", err)
	}

	log.Println("Data restoration complete.")
}

// connect opens a ClickHouse connection.
func connect(addr, db, user, pass string) (driver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{Database: db, Username: user, Password: pass},
	})
}

// syncCandles backfills missing rows in candles table.
func syncCandles(ctx context.Context, src, tgt driver.Conn) error {
	var minSrc, maxSrc, minTgt, maxTgt uint64
	if err := src.QueryRow(ctx,
		"SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM candles").
		Scan(&minSrc, &maxSrc); err != nil {
		return fmt.Errorf("source range: %w", err)
	}
	if err := tgt.QueryRow(ctx,
		"SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM candles").
		Scan(&minTgt, &maxTgt); err != nil {
		minTgt = maxSrc + 1
	}

	existing := make(map[string]struct{})
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, err := tgt.Query(ctx,
			"SELECT toUInt64(unix), exchange, symbol FROM candles WHERE toUInt64(unix) BETWEEN ? AND ?", start, end)
		if err != nil {
			return fmt.Errorf("target existing query: %w", err)
		}
		for rows.Next() {
			var ts uint64
			var exch, sym string
			rows.Scan(&ts, &exch, &sym)
			existing[fmt.Sprintf("%d:%s:%s", ts, exch, sym)] = struct{}{}
		}
		rows.Close()
	}

	insertStmt := `INSERT INTO candles
		(unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final, exchange, symbol)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	// backfill windows
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, err := src.Query(ctx,
			`SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final, exchange, symbol
			 FROM candles
			 WHERE toUInt64(unix) BETWEEN ? AND ?
			 ORDER BY unix ASC`, start, end)
		if err != nil {
			return fmt.Errorf("source scan: %w", err)
		}

		batch, err := tgt.PrepareBatch(ctx, insertStmt)
		if err != nil {
			return fmt.Errorf("prepare batch: %w", err)
		}

		count := 0
		for rows.Next() {
			var ts time.Time
			var o, c, h, l, vb, vs float64
			var tb, tsell int64
			var fin bool
			var exch, sym string

			if err := rows.Scan(&ts, &o, &c, &h, &l, &vb, &vs, &tb, &tsell, &fin, &exch, &sym); err != nil {
				return fmt.Errorf("scan source row: %w", err)
			}
			key := fmt.Sprintf("%d:%s:%s", ts.Unix(), exch, sym)
			if _, found := existing[key]; !found {
				if err := batch.Append(ts, o, c, h, l, vb, vs, tb, tsell, fin, exch, sym); err != nil {
					return fmt.Errorf("append batch: %w", err)
				}
				count++
			}
		}
		batch.Send()
		rows.Close()
		log.Printf("candles: inserted %d rows in window %d–%d", count, start, end)
	}

	// append anything past maxTgt
	if maxTgt < maxSrc {
		rows, err := src.Query(ctx,
			`SELECT unix, open, close, high, low, vbuy, vsell, tbuy, tsell, final, exchange, symbol
			 FROM candles
			 WHERE toUInt64(unix) > ?
			 ORDER BY unix ASC`, maxTgt)
		if err != nil {
			return fmt.Errorf("scan new data: %w", err)
		}
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var o, c, h, l, vb, vs float64
			var tb, tsell int64
			var fin bool
			var exch, sym string
			rows.Scan(&ts, &o, &c, &h, &l, &vb, &vs, &tb, &tsell, &fin, &exch, &sym)
			batch.Append(ts, o, c, h, l, vb, vs, tb, tsell, fin, exch, sym)
			count++
		}
		batch.Send()
		log.Printf("candles: appended %d new rows past %d", count, maxTgt)
	}

	return nil
}

// syncVolumes backfills missing rows in volumes table.
func syncVolumes(ctx context.Context, src, tgt driver.Conn) error {
	var minSrc, maxSrc, minTgt, maxTgt uint64
	src.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM volumes").
		Scan(&minSrc, &maxSrc)
	tgt.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM volumes").
		Scan(&minTgt, &maxTgt)

	existing := make(map[string]struct{})
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := tgt.Query(ctx,
			"SELECT toUInt64(unix), exchange, symbol FROM volumes WHERE toUInt64(unix) BETWEEN ? AND ?", start, end)
		for rows.Next() {
			var ts uint64
			var exch, sym string
			rows.Scan(&ts, &exch, &sym)
			existing[fmt.Sprintf("%d:%s:%s", ts, exch, sym)] = struct{}{}
		}
		rows.Close()
	}

	insertStmt := `INSERT INTO volumes
		(unix, exchange, symbol, prices, buys, sells, price_group, final)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := src.Query(ctx,
			`SELECT unix, exchange, symbol, prices, buys, sells, price_group, final
			 FROM volumes
			 WHERE toUInt64(unix) BETWEEN ? AND ?
			 ORDER BY unix ASC`, start, end)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var exch, sym string
			var prices, buys, sells []float64
			var pg float64
			var fin bool
			rows.Scan(&ts, &exch, &sym, &prices, &buys, &sells, &pg, &fin)
			key := fmt.Sprintf("%d:%s:%s", ts.Unix(), exch, sym)
			if _, ok := existing[key]; !ok {
				batch.Append(ts, exch, sym, prices, buys, sells, pg, fin)
				count++
			}
		}
		batch.Send()
		rows.Close()
		log.Printf("volumes: inserted %d rows in window %d–%d", count, start, end)
	}
	if maxTgt < maxSrc {
		rows, _ := src.Query(ctx,
			`SELECT unix, exchange, symbol, prices, buys, sells, price_group, final
			 FROM volumes
			 WHERE toUInt64(unix) > ?
			 ORDER BY unix ASC`, maxTgt)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var exch, sym string
			var prices, buys, sells []float64
			var pg float64
			var fin bool
			rows.Scan(&ts, &exch, &sym, &prices, &buys, &sells, &pg, &fin)
			batch.Append(ts, exch, sym, prices, buys, sells, pg, fin)
			count++
		}
		batch.Send()
		log.Printf("volumes: appended %d new rows past %d", count, maxTgt)
	}
	return nil
}

// syncHeatmaps backfills missing rows in heatmaps table.
func syncHeatmaps(ctx context.Context, src, tgt driver.Conn) error {
	var minSrc, maxSrc, minTgt, maxTgt uint64
	src.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM heatmaps").
		Scan(&minSrc, &maxSrc)
	tgt.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM heatmaps").
		Scan(&minTgt, &maxTgt)

	existing := make(map[string]struct{})
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := tgt.Query(ctx,
			"SELECT toUInt64(unix), exchange, symbol FROM heatmaps WHERE toUInt64(unix) BETWEEN ? AND ?", start, end)
		for rows.Next() {
			var ts uint64
			var exch, sym string
			rows.Scan(&ts, &exch, &sym)
			existing[fmt.Sprintf("%d:%s:%s", ts, exch, sym)] = struct{}{}
		}
		rows.Close()
	}

	insertStmt := `INSERT INTO heatmaps
		(unix, price_group, exchange, symbol, prices, sizes, min_price, max_price, max_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := src.Query(ctx,
			`SELECT unix, price_group, exchange, symbol, prices, sizes, min_price, max_price, max_size
			 FROM heatmaps
			 WHERE toUInt64(unix) BETWEEN ? AND ?
			 ORDER BY unix ASC`, start, end)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var pg, minP, maxP, maxS float64
			var exch, sym string
			var prices, sizes []float64
			rows.Scan(&ts, &pg, &exch, &sym, &prices, &sizes, &minP, &maxP, &maxS)
			key := fmt.Sprintf("%d:%s:%s", ts.Unix(), exch, sym)
			if _, ok := existing[key]; !ok {
				batch.Append(ts, pg, exch, sym, prices, sizes, minP, maxP, maxS)
				count++
			}
		}
		batch.Send()
		rows.Close()
		log.Printf("heatmaps: inserted %d rows in window %d–%d", count, start, end)
	}
	if maxTgt < maxSrc {
		rows, _ := src.Query(ctx,
			`SELECT unix, price_group, exchange, symbol, prices, sizes, min_price, max_price, max_size
			 FROM heatmaps
			 WHERE toUInt64(unix) > ?
			 ORDER BY unix ASC`, maxTgt)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var pg, minP, maxP, maxS float64
			var exch, sym string
			var prices, sizes []float64
			rows.Scan(&ts, &pg, &exch, &sym, &prices, &sizes, &minP, &maxP, &maxS)
			batch.Append(ts, pg, exch, sym, prices, sizes, minP, maxP, maxS)
			count++
		}
		batch.Send()
		log.Printf("heatmaps: appended %d new rows past %d", count, maxTgt)
	}
	return nil
}

// syncStats backfills missing rows in stats table.
func syncStats(ctx context.Context, src, tgt driver.Conn) error {
	var minSrc, maxSrc, minTgt, maxTgt uint64
	src.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM stats").
		Scan(&minSrc, &maxSrc)
	tgt.QueryRow(ctx, "SELECT toUInt64(min(unix)), toUInt64(max(unix)) FROM stats").
		Scan(&minTgt, &maxTgt)

	existing := make(map[string]struct{})
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := tgt.Query(ctx,
			"SELECT toUInt64(unix), exchange, symbol, timeframe FROM stats WHERE toUInt64(unix) BETWEEN ? AND ?", start, end)
		for rows.Next() {
			var ts, tf uint64
			var exch, sym string
			rows.Scan(&ts, &exch, &sym, &tf)
			existing[fmt.Sprintf("%d:%s:%s:%d", ts, exch, sym, tf)] = struct{}{}
		}
		rows.Close()
	}

	insertStmt := `INSERT INTO stats
		(unix, exchange, symbol, timeframe, liq_vsell, liq_vbuy, mark_price, funding, tbuy, tsell, final)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for start := minSrc; start <= maxSrc; start += uint64(chunkSize.Seconds()) {
		end := start + uint64(chunkSize.Seconds()) - 1
		if end > maxSrc {
			end = maxSrc
		}
		rows, _ := src.Query(ctx,
			`SELECT unix, exchange, symbol, timeframe, liq_vsell, liq_vbuy, mark_price, funding, tbuy, tsell, final
			 FROM stats
			 WHERE toUInt64(unix) BETWEEN ? AND ?
			 ORDER BY unix ASC`, start, end)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var exch, sym string
			var tf int64
			var ls, lb, mp, f float64
			var tb, tsell int64
			var fin bool
			rows.Scan(&ts, &exch, &sym, &tf, &ls, &lb, &mp, &f, &tb, &tsell, &fin)
			key := fmt.Sprintf("%d:%s:%s:%d", ts.Unix(), exch, sym, tf)
			if _, ok := existing[key]; !ok {
				batch.Append(ts, exch, sym, tf, ls, lb, mp, f, tb, tsell, fin)
				count++
			}
		}
		batch.Send()
		rows.Close()
		log.Printf("stats: inserted %d rows in window %d–%d", count, start, end)
	}
	if maxTgt < maxSrc {
		rows, _ := src.Query(ctx,
			`SELECT unix, exchange, symbol, timeframe, liq_vsell, liq_vbuy, mark_price, funding, tbuy, tsell, final
			 FROM stats
			 WHERE toUInt64(unix) > ?
			 ORDER BY unix ASC`, maxTgt)
		batch, _ := tgt.PrepareBatch(ctx, insertStmt)
		count := 0
		for rows.Next() {
			var ts time.Time
			var exch, sym string
			var tf int64
			var ls, lb, mp, f float64
			var tb, tsell int64
			var fin bool
			rows.Scan(&ts, &exch, &sym, &tf, &ls, &lb, &mp, &f, &tb, &tsell, &fin)
			batch.Append(ts, exch, sym, tf, ls, lb, mp, f, tb, tsell, fin)
			count++
		}
		batch.Send()
		log.Printf("stats: appended %d new rows past %d", count, maxTgt)
	}
	return nil
}
