package replay

import (
	"strings"

	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
)

func lineSHA256(data []byte) string {
	return sharedhash.HashBytes(data)
}

func sameSHA256(expected, got string) bool {
	return strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(got))
}
