package app

import (
	"math"
	"testing"
)

func assertClose(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %f, want %f (tol %f)", name, got, want, tol)
	}
}

func TestSpreadBps(t *testing.T) {
	tests := []struct {
		name           string
		bid, ask, want float64
	}{
		{"normal", 100.0, 100.10, 10.0}, // 0.10 / 100.05 * 10000 ≈ 9.995
		{"wide", 50.0, 51.0, 198.020},   // 1.0 / 50.5 * 10000
		{"zero mid", 0, 0, 0},
		{"negative mid", -10, -20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SpreadBps(tt.bid, tt.ask)
			assertClose(t, "SpreadBps", got, tt.want, 0.1)
		})
	}
}

func TestSpreadAbsolute(t *testing.T) {
	got := SpreadAbsolute(100.0, 100.50)
	assertClose(t, "SpreadAbsolute", got, 0.50, 1e-9)
}

func TestMidPrice(t *testing.T) {
	got := MidPrice(100.0, 100.50)
	assertClose(t, "MidPrice", got, 100.25, 1e-9)
}

func TestDepthImbalance(t *testing.T) {
	tests := []struct {
		name           string
		bid, ask, want float64
	}{
		{"balanced", 100, 100, 0},
		{"bid heavy", 150, 50, 0.5},
		{"ask heavy", 50, 150, -0.5},
		{"zero depth", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DepthImbalance(tt.bid, tt.ask)
			assertClose(t, "DepthImbalance", got, tt.want, 1e-9)
		})
	}
}

func TestAggressorDelta(t *testing.T) {
	assertClose(t, "AggressorDelta", AggressorDelta(100, 60), 40, 1e-9)
	assertClose(t, "AggressorDelta negative", AggressorDelta(30, 80), -50, 1e-9)
}

func TestZScore(t *testing.T) {
	tests := []struct {
		name                      string
		value, mean, stddev, want float64
	}{
		{"normal", 10, 5, 2, 2.5},
		{"zero stddev", 10, 5, 0, 0},
		{"negative stddev", 10, 5, -1, 0},
		{"NaN stddev", 10, 5, math.NaN(), 0},
		{"Inf stddev", 10, 5, math.Inf(1), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZScore(tt.value, tt.mean, tt.stddev)
			assertClose(t, "ZScore", got, tt.want, 1e-9)
		})
	}
}
