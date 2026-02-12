package ws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
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

// WsState reports websocket consumer lifecycle state changes.
type WsState struct {
	Exchange   string
	BucketID   int64
	ConsumerID string
	Endpoint   string
	Status     string
	Err        error
	At         time.Time
}

// WsMessage reports websocket payload data from a consumer.
type WsMessage struct {
	Exchange   string
	BucketID   int64
	ConsumerID string
	Endpoint   string
	Data       []byte
	RecvAt     time.Time
}

// WsError reports websocket errors with a structured kind.
type WsError struct {
	Exchange   string
	BucketID   int64
	ConsumerID string
	Endpoint   string
	Kind       string
	Err        error
	At         time.Time
}

type ConsumerConfig struct {
	Exchange             string
	Endpoint             string
	SubscriptionMessages [][]byte
	Heartbeat            Heartbeat
	BucketID             int64
	ConsumerID           string
	SendTo               *actor.PID
}

type consumerFailure struct {
	kind string
	err  error
}

type wsConn interface {
	WriteMessage(messageType int, data []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	ReadMessage() (messageType int, p []byte, err error)
	SetReadDeadline(t time.Time) error
	SetPingHandler(h func(appData string) error)
	SetPongHandler(h func(appData string) error)
	Close() error
	WriteJSON(v any) error
}

type gorillaConn struct {
	conn *websocket.Conn
}

func (g *gorillaConn) WriteMessage(messageType int, data []byte) error {
	return g.conn.WriteMessage(messageType, data)
}

func (g *gorillaConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return g.conn.WriteControl(messageType, data, deadline)
}

func (g *gorillaConn) ReadMessage() (messageType int, p []byte, err error) {
	return g.conn.ReadMessage()
}

func (g *gorillaConn) SetReadDeadline(t time.Time) error {
	return g.conn.SetReadDeadline(t)
}

func (g *gorillaConn) SetPingHandler(h func(appData string) error) {
	g.conn.SetPingHandler(h)
}

func (g *gorillaConn) SetPongHandler(h func(appData string) error) {
	g.conn.SetPongHandler(h)
}

func (g *gorillaConn) Close() error {
	return g.conn.Close()
}

func (g *gorillaConn) WriteJSON(v any) error {
	return g.conn.WriteJSON(v)
}

var consumerDial = func(ctx context.Context, url string) (wsConn, error) {
	conn, err := Dial(ctx, url)
	if err != nil {
		return nil, err
	}
	return &gorillaConn{conn: conn}, nil
}

var consumerDialMu sync.RWMutex

type Consumer struct {
	c      *actor.Context
	config ConsumerConfig

	ctx    context.Context
	cancel context.CancelFunc

	connMu sync.RWMutex
	conn   wsConn

	writeMu  sync.Mutex
	quitch   chan struct{}
	stopOnce sync.Once

	failureOnce sync.Once

	nowFn          func() time.Time
	keepaliveEvery time.Duration
	readTimeout    time.Duration
}

func NewConsumer(config ConsumerConfig) actor.Producer {
	return func() actor.Receiver {
		return &Consumer{
			config:         config,
			quitch:         make(chan struct{}),
			nowFn:          time.Now,
			keepaliveEvery: time.Minute,
			readTimeout:    2 * time.Minute,
		}
	}
}

func (c *Consumer) Receive(ac *actor.Context) {
	switch msg := ac.Message().(type) {
	case actor.Started:
		c.c = ac
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.emitState("starting", nil)
		go c.connect()

	case actor.Stopped:
		c.Stop()
		c.emitState("closed", nil)

	case consumerFailure:
		c.handleFailure(msg.kind, msg.err)
	}
}

func (c *Consumer) connect() {
	c.emitState("dialing", nil)
	consumerDialMu.RLock()
	dialFn := consumerDial
	consumerDialMu.RUnlock()
	conn, err := dialFn(c.ctx, c.config.Endpoint)
	if err != nil {
		c.reportFailure("dial", err)
		return
	}

	if c.isStopped() {
		_ = conn.Close()
		return
	}

	c.setConn(conn)
	if err := conn.SetReadDeadline(c.now().Add(c.readTimeout)); err != nil {
		c.reportFailure("read_deadline", err)
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(c.now().Add(c.readTimeout))
	})

	c.emitState("connected", nil)

	for _, msg := range c.config.SubscriptionMessages {
		if err := c.writeMessage(websocket.TextMessage, msg); err != nil {
			c.reportFailure("subscribe", err)
			return
		}
	}
	if len(c.config.SubscriptionMessages) > 0 {
		c.emitState("subscribed", nil)
	}

	donech := make(chan struct{})
	go c.handleKeepalive(donech)
	go c.handleHeartbeat(donech)

	err = c.readLoop()
	close(donech)

	if err != nil {
		c.reportFailure("read", err)
	}
}

// Stop cancels the consumer context and closes the websocket connection once.
func (c *Consumer) Stop() {
	c.stopOnce.Do(func() {
		close(c.quitch)
		if c.cancel != nil {
			c.cancel()
		}
		_ = c.writeControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"), c.now().Add(time.Second))
		if err := c.closeConn(); err != nil {
			slog.Error("error closing websocket connection", "err", err)
		}
	})
}

func (c *Consumer) handleKeepalive(donech chan struct{}) {
	ticker := time.NewTicker(c.keepaliveEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.writeControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second)); err != nil {
				c.reportFailure("pingpong", err)
				return
			}
		case <-donech:
			return
		case <-c.quitch:
			return
		case <-c.ctx.Done():
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
			if err := c.writeMessage(websocket.TextMessage, c.config.Heartbeat.Message); err != nil {
				c.reportFailure("heartbeat", err)
				return
			}
		case <-donech:
			return
		case <-c.quitch:
			return
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Consumer) readLoop() error {
	for {
		select {
		case <-c.quitch:
			return nil
		case <-c.ctx.Done():
			return nil
		default:
		}

		conn := c.getConn()
		if conn == nil {
			if c.isStopped() {
				return nil
			}
			return fmt.Errorf("connection is nil")
		}

		_, msg, err := conn.ReadMessage()
		if err != nil {
			if c.isStopped() || c.ctx.Err() != nil || isExpectedReadClose(err) {
				return nil
			}
			return err
		}

		c.emitMessage(msg)
	}
}

func (c *Consumer) setConn(conn wsConn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *Consumer) getConn() wsConn {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.conn
}

func (c *Consumer) closeConn() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Consumer) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn := c.getConn()
	if conn == nil {
		return fmt.Errorf("attempting write on nil connection")
	}
	return conn.WriteMessage(messageType, data)
}

func (c *Consumer) writeControl(messageType int, data []byte, deadline time.Time) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn := c.getConn()
	if conn == nil {
		return fmt.Errorf("attempting write control on nil connection")
	}
	return conn.WriteControl(messageType, data, deadline)
}

func (c *Consumer) reportFailure(kind string, err error) {
	if err == nil || c.c == nil {
		return
	}
	c.c.Send(c.c.PID(), consumerFailure{kind: kind, err: err})
}

func (c *Consumer) handleFailure(kind string, err error) {
	if err == nil {
		return
	}
	c.failureOnce.Do(func() {
		slog.Error("websocket consumer error", "kind", kind, "err", err)
		c.emitError(kind, err)
		c.emitState("error", err)
		c.Stop()
	})
}

func (c *Consumer) emitState(status string, err error) {
	if c.c == nil || c.config.SendTo == nil {
		return
	}
	c.c.Send(c.config.SendTo, &WsState{
		Exchange:   c.config.Exchange,
		BucketID:   c.config.BucketID,
		ConsumerID: c.config.ConsumerID,
		Endpoint:   c.config.Endpoint,
		Status:     status,
		Err:        err,
		At:         c.now(),
	})
}

func (c *Consumer) emitMessage(data []byte) {
	if c.c == nil || c.config.SendTo == nil {
		return
	}
	c.c.Send(c.config.SendTo, &WsMessage{
		Exchange:   c.config.Exchange,
		BucketID:   c.config.BucketID,
		ConsumerID: c.config.ConsumerID,
		Endpoint:   c.config.Endpoint,
		Data:       data,
		RecvAt:     c.now(),
	})
}

func (c *Consumer) emitError(kind string, err error) {
	if c.c == nil || c.config.SendTo == nil {
		return
	}
	c.c.Send(c.config.SendTo, &WsError{
		Exchange:   c.config.Exchange,
		BucketID:   c.config.BucketID,
		ConsumerID: c.config.ConsumerID,
		Endpoint:   c.config.Endpoint,
		Kind:       kind,
		Err:        err,
		At:         c.now(),
	})
}

func (c *Consumer) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c *Consumer) isStopped() bool {
	select {
	case <-c.quitch:
		return true
	default:
		return false
	}
}

func (c *Consumer) WriteJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn := c.getConn()
	if conn == nil {
		return fmt.Errorf("attempting write on nil connection")
	}
	return conn.WriteJSON(v)
}

func isExpectedReadClose(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return true
	}
	return websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}
