package app

import "github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"

func resolveStreamID(event domain.RuleEvent) string {
	if event.StreamID != "" {
		return event.StreamID
	}
	return event.StreamKey()
}
