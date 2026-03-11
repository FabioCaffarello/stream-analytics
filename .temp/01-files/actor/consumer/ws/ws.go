package ws

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/gorilla/websocket"
)

func Dial(ctx context.Context, url string) (*websocket.Conn, error) {
	dialer := &websocket.Dialer{
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
		HandshakeTimeout: 60 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	return conn, err
}

type WebsocketError struct {
	Err error
}

type ConsumerConfig struct {
	Endpoint             string
	SubscriptionMessages [][]byte
	Heartbeat            Heartbeat
	BucketID             int64
	ConsumerID           string
	SendTo               *actor.PID
}

type Consumer struct {
	c        *actor.Context
	config   ConsumerConfig
	conn     *websocket.Conn
	quitch   chan struct{}
	stopOnce sync.Once
}

type WsMessage struct {
	Data   []byte
	RecvAt time.Time
}

func NewConsumer(config ConsumerConfig) actor.Producer {
	return func() actor.Receiver {
		return &Consumer{
			config: config,
			quitch: make(chan struct{}),
		}
	}
}

func (c *Consumer) Receive(ac *actor.Context) {
	switch msg := ac.Message().(type) {
	case actor.Started:
		c.c = ac
		go c.connect(ac)

	case actor.Stopped:
		c.Stop()

	case WebsocketError:
		slog.Error("websocket error", "error", msg.Err)
		c.Stop()
		panic(msg.Err)
	}
}

func (c *Consumer) connect(ac *actor.Context) {
	conn, err := Dial(context.Background(), c.config.Endpoint)
	if err != nil {
		ac.Send(ac.PID(), WebsocketError{Err: err})
		return
	}

	if len(c.config.SubscriptionMessages) > 0 {
		for _, msg := range c.config.SubscriptionMessages {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				slog.Error("failed to send subscription message", "err", err.Error())
				ac.Send(ac.PID(), WebsocketError{Err: err})
				return
			}
		}
	}

	c.conn = conn
	conn.SetPingHandler(func(s string) error {
		return conn.WriteControl(websocket.PongMessage, []byte(s), time.Now().Add(time.Second))
	})

	donech := make(chan struct{})
	go c.handlePingPong(donech)
	go c.handleHeartbeat(donech)

	err = c.readLoop()
	close(donech)
	if err != nil {
		slog.Error("ws read error", "err", err.Error())
		ac.Send(ac.PID(), WebsocketError{Err: err})
		return
	}
}

func (c *Consumer) Stop() {
	c.stopOnce.Do(func() {
		close(c.quitch)
		if c.conn != nil {
			if err := c.conn.Close(); err != nil {
				slog.Error("error closing websocket connection", "err", err.Error())
			} else {
				slog.Info("websocket connection closed")
			}
		}
	})
}

func (c *Consumer) handlePingPong(donech chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(time.Second)); err != nil {
				slog.Error("failed to write pong message", "err", err.Error())
				return
			}
		case <-donech:
			slog.Debug("ws consumer ping pong has stopped", "reason", "donech")
			return
		case <-c.quitch:
			slog.Debug("ws consumer ping pong has stopped", "reason", "quitch")
			return
		}
	}
}

func (c *Consumer) handleHeartbeat(donech chan struct{}) {
	if c.config.Heartbeat.Interval == 0 {
		return
	}

	ticker := time.NewTicker(c.config.Heartbeat.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.conn.WriteMessage(websocket.TextMessage, c.config.Heartbeat.Message); err != nil {
				slog.Error("failed to send heartbeat", "err", err.Error())
				return
			}
		case <-c.quitch:
			slog.Debug("ws consumer heartbeat has stopped", "reason", "quitch")
			return
		case <-donech:
			slog.Debug("ws consumer heartbeat has stopped", "reason", "donech")
			return
		}
	}
}

func (c *Consumer) readLoop() error {
	for {
		select {
		case <-c.quitch:
			slog.Debug("stopping readloop")
			return nil
		default:
			_, msg, err := c.conn.ReadMessage()
			if err != nil {
				return err
			}

			wsMsg := &WsMessage{
				Data:   msg,
				RecvAt: time.Now(),
			}

			c.c.Send(c.config.SendTo, wsMsg)
		}
	}
}

func (c *Consumer) WriteJSON(v any) error {
	if c.conn != nil {
		return c.conn.WriteJSON(v)
	}
	return fmt.Errorf("attempting write on nil connection")
}
