package app

import "github.com/market-raccoon/internal/core/evidence/domain"

func resolveStreamID(event domain.RuleEvent) string {
	if event.StreamID != "" {
		return event.StreamID
	}
	return event.StreamKey()
}
