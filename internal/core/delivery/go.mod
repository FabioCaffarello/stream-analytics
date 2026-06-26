module github.com/FabioCaffarello/stream-analytics/internal/core/delivery

go 1.25.6

require github.com/FabioCaffarello/stream-analytics/internal/shared v0.0.0

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/matttproud/golang_protobuf_extensions/v2 v2.0.0 // indirect
	github.com/prometheus/client_golang v1.18.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.45.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/FabioCaffarello/stream-analytics/internal/core/marketdata => ../../../internal/core/marketdata

replace (
	github.com/FabioCaffarello/stream-analytics/internal/core/evidence => ../../../internal/core/evidence
	github.com/FabioCaffarello/stream-analytics/internal/core/insights => ../../../internal/core/insights
	github.com/FabioCaffarello/stream-analytics/internal/core/marketmodel => ../marketmodel
	github.com/FabioCaffarello/stream-analytics/internal/shared => ../../../internal/shared
)

replace github.com/FabioCaffarello/stream-analytics/internal/actors => ../../actors
