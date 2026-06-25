package contracts

// LiquidityEvidenceMetric is one metric in liquidity.evidence payloads.
type LiquidityEvidenceMetric struct {
	Key   string
	Value float64
}

// LiquidityInputWatermark captures source sequence range used to build evidence.
type LiquidityInputWatermark struct {
	SeqStart int64
	SeqEnd   int64
}

// LiquidityEvidenceV1 is the shared wire DTO for liquidity.evidence v1.
type LiquidityEvidenceV1 struct {
	EvidenceType string
	TsIngestMs   int64
	Venue        string
	Symbol       string
	WindowMs     int64
	Severity     string
	Confidence   float64
	Metrics      []LiquidityEvidenceMetric
	Explain      []string
	Version      int32
	StreamID     string
	Seq          int64
	Watermark    LiquidityInputWatermark
}
