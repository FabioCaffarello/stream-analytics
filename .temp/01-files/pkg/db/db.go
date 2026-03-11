package db

import (
	"context"
	"marketmonkey/event"
)

type Client interface {
	InsertCandles(ctx context.Context, pair *event.Pair, candles []*event.Candle) error
	InsertVolume(ctx context.Context, volume *event.Volume) error
	InsertVolumes(ctx context.Context, volumes []*event.Volume) error
	InsertHeatmap(ctx context.Context, heatmap *event.Heatmap) error
	InsertStats(ctx context.Context, stats *event.Stats) error

	GetFirstCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error)
	GetLastCandle(ctx context.Context, pair *event.Pair) (*event.Candle, error)
	GetCandles(pair *event.Pair, from, to, timeframe int64) ([]*event.Candle, error)
	GetStats(pair *event.Pair, from, to, timeframe int64) ([]*event.Stat, error)
	GetAllCandles(pair *event.Pair) ([]*event.Candle, error)
	GetFirstVolume(ctx context.Context, pair *event.Pair) (*event.Volume, error)
	GetVolumes(pair *event.Pair, from, to, timeframe int64) ([]*event.Volume, error)
	GetHeatmaps(pair *event.Pair, from, to, timeframe int64) ([]*event.Heatmap, error)
}
