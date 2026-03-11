package act

import (
	"fmt"
	"marketmonkey/event"

	"github.com/anthdm/hollywood/actor"
)

func MakePublishID(pair *event.Pair) string {
	return fmt.Sprintf("publish/%s/%s", pair.Exchange, pair.Symbol)
}

func MakePublishPID(c *actor.Context, pair *event.Pair) *actor.PID {
	return actor.NewPID(c.Engine().Address(), MakePublishID(pair))
}

func SerializePID(pid *actor.PID) string {
	return fmt.Sprintf("%s_%s", pid.Address, pid.ID)
}
