package bootstrap

import (
	"os"
	"os/signal"
	"syscall"
)

// SignalChannel returns a channel that receives SIGINT or SIGTERM.
// Callers can select on this along with other error channels.
func SignalChannel() <-chan os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit
}
