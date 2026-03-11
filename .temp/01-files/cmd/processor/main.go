package main

import (
	"flag"
	"fmt"
	"log"
	"marketmonkey/actor/processor"
	"marketmonkey/cmd"
	"math"
	"os"

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

	id := fmt.Sprintf("processor-%s", *exchange)
	pid := e.Spawn(processor.New(*exchange), id,
		actor.WithID(id),
		actor.WithMaxRestarts(math.MaxInt),
	)

	cmd.WaitTillShutdown(e, pid)
}
