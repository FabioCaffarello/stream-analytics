package main

import (
	"log"
	"log/slog"
	"marketmonkey/actor/server"
	"marketmonkey/cmd"
	"os"

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
	httpServerAddr := os.Getenv("HTTP_SERVER_ADDR")
	if len(httpServerAddr) == 0 {
		log.Fatal("you FORGOT to set the HTTP_SERVER_ADDR in the environment")
	}

	dbClient, err := cmd.CreateDBClient()
	if err != nil {
		log.Fatal(err)
	}

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		log.Fatal(err)
	}

	pid := e.Spawn(server.New(httpServerAddr, dbClient), "server")

	cmd.WaitTillShutdown(e, pid)
}
