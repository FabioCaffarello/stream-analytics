package main

import (
	"flag"
	"log"
	"os"

	"marketmonkey/actor/consumer/binance"
	"marketmonkey/actor/consumer/binancef"
	"marketmonkey/actor/consumer/bybit"
	"marketmonkey/actor/consumer/coinbase"
	"marketmonkey/actor/consumer/hyperliquid"
	"marketmonkey/cmd"
	"marketmonkey/config"

	"github.com/anthdm/hollywood/actor"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err.Error())
	}
	consulAddr := os.Getenv("CONSUL_ADDR")
	if len(consulAddr) == 0 {
		log.Fatal("you FORGOT to set the CONSUL_ADDR in the environment")
	}

	natsUrl := os.Getenv("NATS_URL")
	if len(natsUrl) == 0 {
		log.Fatal("you FORGOT to set the NATS_URL in the environment")
	}

	exchange := flag.String("exchange", "binancef", "exchange to consume from")
	flag.Parse()

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		log.Fatal(err.Error())
	}

	var pid *actor.PID
	switch *exchange {
	case config.Binancef:
		pid = e.Spawn(binancef.New(), "consumer", actor.WithID(config.Binancef))
	case config.Hyperliquid:
		pid = e.Spawn(hyperliquid.New(), "consumer", actor.WithID(config.Hyperliquid))
	case config.Bybit:
		pid = e.Spawn(bybit.New(), "consumer", actor.WithID(config.Bybit))
	case config.Binance:
		pid = e.Spawn(binance.New(), "consumer", actor.WithID(config.Binance))
	case config.Coinbase:
		pid = e.Spawn(coinbase.New(), "consumer", actor.WithID(config.Coinbase))
	default:
		log.Fatalf("invalid or unsupported exchange: %s", *exchange)
	}

	cmd.WaitTillShutdown(e, pid)
}
