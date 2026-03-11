package main

import (
	"log"
	"log/slog"
	"os"

	"marketmonkey/actor/store"
	"marketmonkey/cmd"

	"github.com/anthdm/hollywood/actor"
	"github.com/joho/godotenv"
)

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)
	if err := godotenv.Load(); err != nil {
		log.Fatal(err)
	}

	consulAddr := os.Getenv("CONSUL_ADDR")
	if len(consulAddr) == 0 {
		log.Fatal("you FORGOT to set the CONSUL_ADDR in the environment")
	}
	dbClient, err := cmd.CreateDBClient()
	if err != nil {
		log.Fatal(err)
	}

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		log.Fatal(err)
	}

	pid := e.Spawn(store.New(dbClient), "store", actor.WithID("1"))
	cmd.WaitTillShutdown(e, pid)
}
