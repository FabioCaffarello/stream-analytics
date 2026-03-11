# Market Monkey
Better terminal for better decisions.

# How to run the terminal locally (quick start guide)
Market Monkey exists of 2 major parts. The server and the client (desktop or in the browser).


### Server
The server is currently divided in a couple components:
1. Consumers
2. Processors
3. Store
4. Server

### Consumers
Consumers are responsible for fetching data from the data providers (exchanges, brokers, etc). Each consumer has its own data provider will run a [websocket manager](/actor/consumer/ws/manager.go) to manage the websocket subscriptions. 

### Store
The store is responsible for storing all timeseries data. The store subscribes to the store queues, which contains 60s timeseries data. The store will then store this data in the database.

### Server
The server is responsible for authenticating clients and providing them with data over websockets encoded with the CBOR protocol. Not every message is encoded with CBOR. Only the data is encoded. Messages like range requests or health checks are not encoded.

## Self hosting Market Monkey (with Docker)
In the project root there is a `docker-compose.yml` file that bootstraps all services / components needed to successfully run a local or remote version of Market Monkey for personal / internal use.

- timescaledb or clickhouse database for all timeseries data (we recommend the clickhouse engine though)
- caddy (proxy server)
- consul (metrics discovery)
- consumer actors
- processor actors
- store actor
- server actor (authentication and WS stream)
- NATS (messaging queue)

### Starting Market Monkey Server:
```bash
docker compose up -d --build
```

### Stopping Market Monkey Server:
```bash
docker compose down
```

### Full rolling update (mac & linux only):
Currently the service supports a semi rolling update, this script will:
1. Roll each consumer
2. Build processors, wait until 1s past the minute, then restart them
3. Restart store
4. Restart server
```bash
chmod +x roll.sh
./roll.sh
```

### Consumer specific rolling update (mac & linux only):
This will:
1. Build the new image
2. Bring up a new container using the new image
3. Wait for the new container to be ready
4. Stop and remove the old container
```bash
./roll_consumer.sh consumer --exchange binancef
```


> Make sure to always backup your volumes / data before running new releases.

An ideal Docker environment file would look like this:
```env
USE_AUTH=false
DOMAIN="localhost"

DATABASE_ENGINE="clickhouse"

CLICKHOUSE_ADDR="clickhouse:9000" 
CLICKHOUSE_DB="marketmonkey"
CLICKHOUSE_USER="default"
CLICKHOUSE_PASSWORD="password"


CONSUL_ADDR="consul:8500"
HTTP_SERVER_ADDR="0.0.0.0:8000"
NATS_URL="nats://nats:4222"
```

## Self hosting Market Monkey (without Docker)
Just like the (with Docker) section, your best chance is to run consul + caddy + clickhouse / timescaledb + nats inside a Docker container, and run the services without Docker. But if you are brave enough you can install the former directly on your system and run everything without any Docker interaction. That's up to you.

You will need to adapt the .env file
```
USE_AUTH=false
DOMAIN="localhost"

DATABASE_ENGINE="clickhouse"

CLICKHOUSE_ADDR="127.0.0.1:9000" 
CLICKHOUSE_DB="marketmonkey"
CLICKHOUSE_USER="default"
CLICKHOUSE_PASSWORD="password"

CONSUL_ADDR="127.0.0.1:8500"
HTTP_SERVER_ADDR="127.0.0.1:8000"
NATS_URL="nats://127.0.0.1:4222"
```

> NOTE the change from server:port -> 127.0.0.1:port

```
docker compose up consul caddy clickhouse nats -d
```

Now run the store:
```
make store
```

The server
```
make server
```

Consumers
```
go run cmd/consumer/main.go --exchange binancef // (bybit, hyperliquid)
```

Processors
```
go run cmd/processor/main.go --exchange binancef // (bybit, hyperliquid)
```

## Starting the client
Easy peasy
```
make
```

## Adding new tickers to existing markets
In the root of the project there is a config.yml file. Feel free to take a look in the file which will be self explanatory.

## Adding new timeframes
you can go as crazy as you want with the timeframes, as everything get's sampled on the server from trades. The only limit is your CPU ^^.

## Historical data
```
This is postponed, to be continued very soon, pinky swear.
```

## Bugs
Well there will be bugs for sure. You can issue them in the Discord channel. Much appreciated.

