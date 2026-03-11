package main

import (
	"fmt"
	"log"
	"marketmonkey/actor/processor"
	"marketmonkey/actor/store"
	"marketmonkey/cmd"
	"marketmonkey/event"
	"marketmonkey/pkg/history/binancef"
	"os"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/joho/godotenv"
)

var (
	exchange = "binancef"
	symbols  = []string{"BTCUSDT"}
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal(err.Error())
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
		log.Fatal(err.Error())
	}

	storePID := e.Spawn(store.New(dbClient), "store", actor.WithID("1"))
	for _, symbol := range symbols {
		var (
			// 6 months ago
			from = time.Now().AddDate(0, 0, -2)
			// 1 day ago
			to = time.Now().AddDate(0, 0, -1)
		)

		dh := binancef.NewDateHandler(from, to)
		dates := dh.GetDates()

		trades := []*event.Trade{}
		for _, date := range dates {
			zip := binancef.NewBinanceFutureZip(symbol, date.Year, date.Month, date.Day)
			newTrades, err := zip.DownloadAggTrades()
			if err != nil {
				log.Fatal(err.Error())
			}
			trades = append(trades, newTrades...)
		}

		fmt.Println("received total trades", len(trades))

		pid := e.Spawn(processor.New(exchange, false), "backfill-processor", actor.WithID(fmt.Sprintf("backfill-processor-%s", exchange)))
		for _, trade := range trades {
			e.Send(pid, trade)
		}
		<-e.Poison(pid).Done()
	}
	cmd.WaitTillShutdown(e, storePID)
}
