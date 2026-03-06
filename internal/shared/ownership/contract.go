package ownership

import (
	"strconv"
	"strings"

	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

// Subsystem identifies the stream ownership domain.
type Subsystem string

const (
	SubsystemSignals    Subsystem = "signals"
	SubsystemStrategist Subsystem = "strategist"
	SubsystemExecution  Subsystem = "execution"
	SubsystemPortfolio  Subsystem = "portfolio"
	SubsystemDelivery   Subsystem = "delivery"
)

// StreamKey is the canonical ownership key payload.
type StreamKey struct {
	Venue      string
	Instrument string
	Channel    string
	Timeframe  string
}

func canonicalLower(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func canonicalUpper(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}

func canonicalSubsystem(v Subsystem) string {
	s := canonicalLower(string(v))
	if s == "" {
		return "unknown"
	}
	return s
}

// CanonicalLabel returns a stable label suitable for logs/map keys.
func CanonicalLabel(key StreamKey) string {
	return canonicalLower(key.Venue) + "|" + canonicalUpper(key.Instrument) + "|" + canonicalLower(key.Channel) + "|" + canonicalLower(key.Timeframe)
}

// ShardKey returns a deterministic shard key for the subsystem/key pair.
func ShardKey(subsystem Subsystem, key StreamKey) uint64 {
	return sharedhash.SumFieldsFast64(
		canonicalSubsystem(subsystem),
		canonicalLower(key.Venue),
		canonicalUpper(key.Instrument),
		canonicalLower(key.Channel),
		canonicalLower(key.Timeframe),
	)
}

// OwnerReplica returns the owner replica for this stream key.
func OwnerReplica(subsystem Subsystem, key StreamKey, replicaCount int) int {
	if replicaCount <= 1 {
		return 0
	}
	ownerStr := strconv.FormatUint(ShardKey(subsystem, key)%uint64(replicaCount), 10)
	owner, err := strconv.Atoi(ownerStr)
	if err != nil || owner < 0 || owner >= replicaCount {
		return 0
	}
	return owner
}
