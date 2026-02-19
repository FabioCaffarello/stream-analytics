package timescale

import "sync/atomic"

const (
	// AdapterModeStubMemory marks this package as an in-memory stub adapter.
	AdapterModeStubMemory = "stub-memory"
	// AdapterModePGX marks this package as pgx-backed.
	AdapterModePGX = "pgx"
)

var (
	productionReady atomic.Bool
	adapterMode     atomic.Value
)

func init() {
	adapterMode.Store(AdapterModeStubMemory)
}

// AdapterMode returns the operational mode for this adapter package.
func AdapterMode() string {
	if mode, ok := adapterMode.Load().(string); ok && mode != "" {
		return mode
	}
	return AdapterModeStubMemory
}

// IsProductionReady reports whether this adapter is a production-ready durable store.
func IsProductionReady() bool {
	return productionReady.Load()
}

// SetProductionReady marks the adapter as production-ready using the provided mode.
func SetProductionReady(mode string) {
	if mode == "" {
		mode = AdapterModePGX
	}
	adapterMode.Store(mode)
	productionReady.Store(true)
}

// SetStubMode marks the adapter as non-production and sets the runtime mode.
func SetStubMode(mode string) {
	if mode == "" {
		mode = AdapterModeStubMemory
	}
	adapterMode.Store(mode)
	productionReady.Store(false)
}
