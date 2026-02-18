package timescale

const (
	// AdapterModeStubMemory marks this package as an in-memory stub adapter.
	AdapterModeStubMemory = "stub-memory"
)

// AdapterMode returns the operational mode for this adapter package.
func AdapterMode() string {
	return AdapterModeStubMemory
}

// IsProductionReady reports whether this adapter is a production-ready durable store.
func IsProductionReady() bool {
	return false
}
