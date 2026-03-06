package domain

import "math"

// DeltaVolumeWindowV1 is the deterministic delta-volume projection for one tape window.
type DeltaVolumeWindowV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	BuyVolume     float64
	SellVolume    float64
	DeltaVolume   float64
	Seq           int64
	TsIngestMs    int64
}

// CVDWindowV1 is the deterministic cumulative volume delta projection for one tape window.
type CVDWindowV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	DeltaVolume   float64
	CVD           float64
	Seq           int64
	TsIngestMs    int64
}

// BarStatsWindowV1 is the deterministic bar-statistics projection for one tape window.
type BarStatsWindowV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	TradeCount    int64
	BuyCount      int64
	SellCount     int64
	TotalVolume   float64
	BuyVolume     float64
	SellVolume    float64
	VwapPrice     float64
	LastPrice     float64
	MaxPrice      float64
	MinPrice      float64
	Imbalance     float64
	IsBurst       bool
	Seq           int64
	TsIngestMs    int64
}

// OpenInterestWindowV1 is the deterministic open-interest projection for one input update.
type OpenInterestWindowV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	OpenInterest  float64
	Delta         float64
	DeltaPct      float64
	Seq           int64
	TsIngestMs    int64
}

// DeltaVolumeClosed is emitted when one delta-volume window is materialized.
type DeltaVolumeClosed struct {
	Window DeltaVolumeWindowV1
}

// EventName returns the stable event name.
func (DeltaVolumeClosed) EventName() string { return "DeltaVolumeClosed" }

// CVDClosed is emitted when one cumulative-volume-delta window is materialized.
type CVDClosed struct {
	Window CVDWindowV1
}

// EventName returns the stable event name.
func (CVDClosed) EventName() string { return "CVDClosed" }

// BarStatsClosed is emitted when one bar-statistics window is materialized.
type BarStatsClosed struct {
	Window BarStatsWindowV1
}

// EventName returns the stable event name.
func (BarStatsClosed) EventName() string { return "BarStatsClosed" }

// OpenInterestClosed is emitted when one open-interest window is materialized.
type OpenInterestClosed struct {
	Window OpenInterestWindowV1
}

// EventName returns the stable event name.
func (OpenInterestClosed) EventName() string { return "OpenInterestClosed" }

// NewDeltaVolumeWindowV1 projects one tape window into delta-volume.
func NewDeltaVolumeWindowV1(win TapeWindowV1) DeltaVolumeWindowV1 {
	return DeltaVolumeWindowV1{
		Venue:         win.Venue,
		Instrument:    win.Instrument,
		Timeframe:     win.Timeframe,
		WindowStartTs: win.WindowStartTs,
		WindowEndTs:   win.WindowEndTs,
		BuyVolume:     win.BuyVolume,
		SellVolume:    win.SellVolume,
		DeltaVolume:   win.BuyVolume - win.SellVolume,
		Seq:           win.LastSeq,
		TsIngestMs:    win.WindowEndTs,
	}
}

// NewCVDWindowV1 projects one delta-volume window into cumulative volume delta.
func NewCVDWindowV1(delta DeltaVolumeWindowV1, cvd float64) CVDWindowV1 {
	return CVDWindowV1{
		Venue:         delta.Venue,
		Instrument:    delta.Instrument,
		Timeframe:     delta.Timeframe,
		WindowStartTs: delta.WindowStartTs,
		WindowEndTs:   delta.WindowEndTs,
		DeltaVolume:   delta.DeltaVolume,
		CVD:           cvd,
		Seq:           delta.Seq,
		TsIngestMs:    delta.TsIngestMs,
	}
}

// NewBarStatsWindowV1 projects one tape window into bar statistics.
func NewBarStatsWindowV1(win TapeWindowV1, isBurst bool) BarStatsWindowV1 {
	return BarStatsWindowV1{
		Venue:         win.Venue,
		Instrument:    win.Instrument,
		Timeframe:     win.Timeframe,
		WindowStartTs: win.WindowStartTs,
		WindowEndTs:   win.WindowEndTs,
		TradeCount:    win.TradeCount,
		BuyCount:      win.BuyCount,
		SellCount:     win.SellCount,
		TotalVolume:   win.TotalVolume,
		BuyVolume:     win.BuyVolume,
		SellVolume:    win.SellVolume,
		VwapPrice:     win.VwapPrice,
		LastPrice:     win.LastPrice,
		MaxPrice:      win.MaxPrice,
		MinPrice:      win.MinPrice,
		Imbalance:     win.Imbalance(),
		IsBurst:       isBurst,
		Seq:           win.LastSeq,
		TsIngestMs:    win.WindowEndTs,
	}
}

// BuildOpenInterestWindowV1 computes deterministic open-interest deltas.
func BuildOpenInterestWindowV1(
	venue, instrument string,
	seq, tsIngest, timestamp int64,
	openInterest, prevOpenInterest float64,
	hasPrev bool,
) OpenInterestWindowV1 {
	delta := 0.0
	deltaPct := 0.0
	if hasPrev {
		delta = openInterest - prevOpenInterest
		if prevOpenInterest > 0 {
			deltaPct = delta / prevOpenInterest
		}
	}
	windowTs := timestamp
	if windowTs <= 0 {
		windowTs = tsIngest
	}
	if windowTs <= 0 {
		windowTs = 0
	}
	if math.IsNaN(deltaPct) || math.IsInf(deltaPct, 0) {
		deltaPct = 0
	}
	return OpenInterestWindowV1{
		Venue:         venue,
		Instrument:    instrument,
		Timeframe:     "raw",
		WindowStartTs: windowTs,
		WindowEndTs:   windowTs,
		OpenInterest:  openInterest,
		Delta:         delta,
		DeltaPct:      deltaPct,
		Seq:           seq,
		TsIngestMs:    tsIngest,
	}
}
