package ws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
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
	UptimeSec  float64
	Reconnects int
	Reason     string
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
	Reconnect            ReconnectPolicy
	BucketID             int64
	ConsumerID           string
	SendTo               *actor.PID
}

type ReconnectPolicy struct {
	BaseBackoff  time.Duration
	MaxBackoff   time.Duration
	Jitter       float64
	RetryBudget  int
	BudgetWindow time.Duration
	Cooldown     time.Duration
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

	nowFn          func() time.Time
	keepaliveEvery time.Duration
	readTimeout    time.Duration
	reconnect      ReconnectPolicy

	retryMu         sync.Mutex
	retryTimestamps []time.Time
	reconnectCount  int
	connStartedAt   time.Time
	failureKind     string
}

func NewConsumer(config ConsumerConfig) actor.Producer {
	return func() actor.Receiver {
		return &Consumer{
			config:         config,
			quitch:         make(chan struct{}),
			nowFn:          time.Now,
			keepaliveEvery: time.Minute,
			readTimeout:    2 * time.Minute,
			reconnect:      withReconnectDefaults(config.Reconnect),
		}
	}
}

func (c *Consumer) Receive(ac *actor.Context) {
	switch ac.Message().(type) {
	case actor.Started:
		c.c = ac
		c.ctx, c.cancel = context.WithCancel(context.Background())
		c.emitState("starting", nil)
		go c.connect()

	case actor.Stopped:
		c.Stop()
		c.emitState("closed", nil)
	}
}

func (c *Consumer) connect() {
	for {
		if c.isStopped() || c.ctx.Err() != nil {
			return
		}
		kind, err := c.connectOnce()
		if err == nil {
			return
		}
		c.emitError(kind, err)
		delay, cooldown := c.nextReconnectDelay()
		c.reconnectCount++
		c.emitState("reconnecting", fmt.Errorf("%s: %w", kind, err))
		if cooldown {
			c.emitState("cooldown", err)
		}
		select {
		case <-time.After(delay):
		case <-c.quitch:
			return
		case <-c.ctx.Done():
			return
		}
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
				c.setFailureKind("heartbeat_timeout")
				_ = c.closeConn()
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
				c.setFailureKind("heartbeat")
				_ = c.closeConn()
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

func (c *Consumer) emitState(status string, err error) {
	if c.c == nil || c.config.SendTo == nil {
		return
	}
	uptime := 0.0
	if !c.connStartedAt.IsZero() {
		uptime = c.now().Sub(c.connStartedAt).Seconds()
	}
	c.c.Send(c.config.SendTo, &WsState{
		Exchange:   c.config.Exchange,
		BucketID:   c.config.BucketID,
		ConsumerID: c.config.ConsumerID,
		Endpoint:   c.config.Endpoint,
		Status:     status,
		Err:        err,
		UptimeSec:  uptime,
		Reconnects: c.reconnectCount,
		Reason:     c.getFailureKind(),
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

func withReconnectDefaults(in ReconnectPolicy) ReconnectPolicy {
	if in.BaseBackoff <= 0 {
		in.BaseBackoff = 500 * time.Millisecond
	}
	if in.MaxBackoff <= 0 {
		in.MaxBackoff = 30 * time.Second
	}
	if in.Jitter < 0 {
		in.Jitter = 0
	}
	if in.Jitter > 1 {
		in.Jitter = 1
	}
	if in.RetryBudget <= 0 {
		in.RetryBudget = 20
	}
	if in.BudgetWindow <= 0 {
		in.BudgetWindow = time.Minute
	}
	if in.Cooldown <= 0 {
		in.Cooldown = 30 * time.Second
	}
	return in
}

func (c *Consumer) connectOnce() (string, error) {
	c.emitState("dialing", nil)
	consumerDialMu.RLock()
	dialFn := consumerDial
	consumerDialMu.RUnlock()
	conn, err := dialFn(c.ctx, c.config.Endpoint)
	if err != nil {
		c.setFailureKind("dial")
		return "dial", err
	}
	if c.isStopped() {
		_ = conn.Close()
		return "", nil
	}

	c.setConn(conn)
	defer func() {
		_ = c.closeConn()
	}()
	c.connStartedAt = c.now()
	c.setFailureKind("read")
	if err := conn.SetReadDeadline(c.now().Add(c.readTimeout)); err != nil {
		c.setFailureKind("read_deadline")
		return "read_deadline", err
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(c.now().Add(c.readTimeout))
	})
	c.emitState("connected", nil)

	for _, msg := range c.config.SubscriptionMessages {
		if err := c.writeMessage(websocket.TextMessage, msg); err != nil {
			c.setFailureKind("subscribe")
			return "subscribe", err
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
	if err == nil {
		return "", nil
	}
	kind := c.getFailureKind()
	if kind == "" {
		kind = classifyReadFailure(err)
	}
	return kind, err
}

func classifyReadFailure(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "read_timeout"
	}
	return "read"
}

func (c *Consumer) setFailureKind(kind string) {
	c.retryMu.Lock()
	defer c.retryMu.Unlock()
	c.failureKind = kind
}

func (c *Consumer) getFailureKind() string {
	c.retryMu.Lock()
	defer c.retryMu.Unlock()
	return c.failureKind
}

func (c *Consumer) nextReconnectDelay() (time.Duration, bool) {
	c.retryMu.Lock()
	defer c.retryMu.Unlock()

	now := c.now()
	windowStart := now.Add(-c.reconnect.BudgetWindow)
	filtered := c.retryTimestamps[:0]
	for _, ts := range c.retryTimestamps {
		if ts.After(windowStart) {
			filtered = append(filtered, ts)
		}
	}
	c.retryTimestamps = filtered
	if len(c.retryTimestamps) >= c.reconnect.RetryBudget {
		c.retryTimestamps = nil
		return c.reconnect.Cooldown, true
	}

	attempt := len(c.retryTimestamps)
	delay := c.reconnect.BaseBackoff << attempt
	if delay > c.reconnect.MaxBackoff {
		delay = c.reconnect.MaxBackoff
	}
	if c.reconnect.Jitter > 0 && delay > 0 {
		f := 1 + ((rand.Float64()*2)-1)*c.reconnect.Jitter
		if f < 0 {
			f = 0
		}
		delay = time.Duration(float64(delay) * f)
	}
	c.retryTimestamps = append(c.retryTimestamps, now)
	return delay, false
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
