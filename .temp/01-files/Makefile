MAIN_PATH=./cmd/client

run:
	odin run client/src -collection=deps=./client/deps -vet -strict-style -out=bin/marketmonkey

server:
	go run ./cmd/server .

consumer:
	go run ./cmd/consumer .

store:
	go run ./cmd/store .

build:
	go build -o bin/consumer ./cmd/consumer
	go build -o bin/store ./cmd/store
	go build -o bin/server ./cmd/server

timescale:
	docker run -d --name timescaledb -p 5432:5432 \
		-e POSTGRES_PASSWORD=marketmonkey \
		-e POSTGRES_USER=postgres \
		-e POSTGRES_DB=postgres \
		-v timescale_data:/var/lib/postgresql/data \
		-v "./sql/init.sql:/docker-entrypoint-initdb.d/init.sql" \
		--restart always \
		timescale/timescaledb:latest-pg15 \
		postgres -c "max_connections=500"

proto:
	protoc --go_out=event --go_opt=paths=source_relative --proto_path=event event/*.proto

.PHONY: all clean build build-all windows linux darwin proto timescale

new-migration:
	@read -p "Enter migration name: " name; \
	timestamp=$$(date +%Y%m%d%H%M%S); \
	mkdir -p sql/migrations; \
	echo "-- Create your UP migration here" > "sql/migrations/$${timestamp}_$${name}.up.sql"; \
	echo "-- Create your DOWN migration here" > "sql/migrations/$${timestamp}_$${name}.down.sql"

