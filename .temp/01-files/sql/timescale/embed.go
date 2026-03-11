package timescale

import (
	"embed"
)

//go:embed *.sql
var FS embed.FS
