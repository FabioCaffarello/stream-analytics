package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	adapterkafka "github.com/market-raccoon/internal/adapters/kafka"
	adapternats "github.com/market-raccoon/internal/adapters/nats"
	"github.com/market-raccoon/internal/application/emulatorruntime"
	"github.com/market-raccoon/internal/application/runtimebootstrap"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/clock"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	bindingName := flag.String("binding", "orders", "runtime binding name")
	scenario := flag.String("scenario", emulatorruntime.ScenarioValid, "scenario to emit: valid|missing_required")
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath)
	if prob != nil {
		fmt.Fprintf(os.Stderr, "emulator: config error: %v\n", prob)
		os.Exit(1)
	}
	if len(cfg.DataPlane.Kafka.Brokers) == 0 {
		fmt.Fprintln(os.Stderr, "emulator: data_plane.kafka.brokers must not be empty")
		os.Exit(1)
	}

	store, p := adapternats.NewRuntimeStore(context.Background(), cfg.JetStream.URL, cfg.DataPlane.StateBucket)
	if p != nil {
		fmt.Fprintf(os.Stderr, "emulator: runtime store error: %v\n", p)
		os.Exit(1)
	}
	defer store.Close()

	writer, p := adapterkafka.NewWriter(adapterkafka.WriterConfig{Brokers: cfg.DataPlane.Kafka.Brokers})
	if p != nil {
		fmt.Fprintf(os.Stderr, "emulator: kafka writer error: %v\n", p)
		os.Exit(1)
	}
	defer writer.Close()

	emitter := emulatorruntime.NewEmitter(runtimebootstrap.New(store), writer, clock.NewSystemClock())
	msg, p := emitter.Emit(context.Background(), *bindingName, *scenario)
	if p != nil {
		fmt.Fprintf(os.Stderr, "emulator: emit failed: %v\n", p)
		os.Exit(1)
	}
	fmt.Printf("binding=%s topic=%s message_id=%s correlation_id=%s\n", msg.Binding, msg.Topic, msg.MessageID, msg.CorrelationID)
}
