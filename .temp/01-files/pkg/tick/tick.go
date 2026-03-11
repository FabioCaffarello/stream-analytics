package tick

import "time"

type Ticker struct {
	Interval time.Duration
	Fn       func(time.Time)
	stop     chan struct{}
}

func NewTicker(interval time.Duration, fn func(time.Time)) *Ticker {
	return &Ticker{
		Interval: interval,
		Fn:       fn,
		stop:     make(chan struct{}),
	}
}

func (t *Ticker) Start() {
	go func() {
		now := time.Now()
		nextTick := now.Truncate(t.Interval).Add(t.Interval)
		wait := nextTick.Sub(now)

		time.Sleep(wait)

		ticker := time.NewTicker(t.Interval)
		defer ticker.Stop()

		// if the ticker was stopped before first tick
		select {
		case <-t.stop:
			return
		default:
			t.Fn(nextTick)
		}

		for {
			select {
			case <-ticker.C:
				nextTick = nextTick.Add(t.Interval)
				t.Fn(nextTick)
			case <-t.stop:
				return
			}
		}
	}()
}

func (t *Ticker) Stop() {
	select {
	case <-t.stop:
		return
	default:
		close(t.stop)
	}
}

type Tickers struct {
	tickers []*Ticker
}

func NewTickers() *Tickers {
	return &Tickers{
		tickers: make([]*Ticker, 0),
	}
}

func (t *Tickers) AddTicker(interval time.Duration, fn func(time.Time)) {
	t.tickers = append(t.tickers, NewTicker(interval, fn))
}

func (t *Tickers) Start() {
	for _, ticker := range t.tickers {
		ticker.Start()
	}
}

func (t *Tickers) Stop() {
	for _, ticker := range t.tickers {
		ticker.Stop()
	}
}
