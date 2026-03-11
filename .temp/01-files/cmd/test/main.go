package main

import (
	"fmt"
	"marketmonkey/pkg/tick"
	"time"
)

func main() {

	ticker := tick.NewTicker(time.Millisecond*100, func(t time.Time) {
		fmt.Println(t.UnixMilli())
	})
	ticker.Start()

	time.Sleep(time.Second * 10)
	ticker.Stop()
}
