module github.com/FabioCaffarello/stream-analytics/internal/application

go 1.25.6

require github.com/FabioCaffarello/stream-analytics/internal/shared v0.0.0

require (
	github.com/google/go-cmp v0.7.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

replace github.com/FabioCaffarello/stream-analytics/internal/shared => ../shared
