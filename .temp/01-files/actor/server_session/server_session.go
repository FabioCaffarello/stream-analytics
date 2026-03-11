package serversession

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"marketmonkey/config"
	"marketmonkey/event"
	"marketmonkey/pkg/db"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"marketmonkey/types"
	"net/http"
	"reflect"
	"time"

	"github.com/anthdm/hollywood/actor"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/gommon/log"
	"github.com/valyala/fastjson"
)

type ServerSession struct {
	conn      *websocket.Conn
	id        uuid.UUID
	ctx       *actor.Context
	routerPID *actor.PID
	dbClient  db.Client

	streams map[nats.Subject]bool
	st      time.Time
}

func New(conn *websocket.Conn, id uuid.UUID, routerPID *actor.PID, dbClient db.Client) actor.Producer {
	return func() actor.Receiver {
		return &ServerSession{
			conn:      conn,
			id:        id,
			routerPID: routerPID,
			dbClient:  dbClient,
			streams:   make(map[nats.Subject]bool),
		}
	}
}

func (s *ServerSession) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		s.st = time.Now()
		s.start(c)
		s.ctx = c
	case actor.Stopped:
		metrics.ReportServerWSConnectionDuration(s.st, "clean")
		for stream := range s.streams {
			s.ctx.Send(s.routerPID, &event.Unsubscribe{
				Subject: stream,
				PID:     s.ctx.PID(),
			})
		}

		s.conn.Close()
	case *event.Trade:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:      event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream:    event.StreamTrades,
			Timeframe: 0,
			Data:      data,
		}
		s.send(payload)

	case *event.GetRange:
		s.getRange(msg)

	case *event.Candles:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:      event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream:    event.StreamCandles,
			Timeframe: msg.Timeframe,
			Data:      data,
		}
		s.send(payload)
	case *event.Heatmaps:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:   event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream: event.StreamHeatmaps,
			Data:   data,
		}
		s.send(payload)
	case *event.Stats:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:      event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream:    event.StreamStats,
			Timeframe: msg.Timeframe,
			Data:      data,
		}
		s.send(payload)
	case *event.LiquidationUpdate:
		slog.Info("liquidation", "pair", msg.Pair.Symbol, "exchange", msg.Pair.Exchange)
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:   event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream: event.StreamLiquidations,
			Data:   data,
		}
		s.send(payload)
	case *event.Orderbook:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:   event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream: event.StreamOrderbook,
			Data:   data,
		}
		s.send(payload)
	case *event.Volumes:
		data, _ := json.Marshal(msg)
		payload := types.WSPayload{
			Pair:      event.NewPair(msg.Pair.Exchange, msg.Pair.Symbol),
			Stream:    event.StreamVolumes,
			Timeframe: msg.Timeframe,
			Data:      data,
		}
		s.send(payload)

	default:
		slog.Error("unknown message", "msg", msg, "type", reflect.TypeOf(msg))
	}
}

func (s *ServerSession) start(_ *actor.Context) {
	go s.readMessages()
}

func (s *ServerSession) readMessages() {
	for {
		_, msg, err := s.conn.ReadMessage()
		if err != nil {
			slog.Warn("error reading from ws connection", "err", err)
			break
		}
		s.parseMessage(msg)
	}
	slog.Debug("client disconnected", "session_id", s.id)
	s.ctx.Engine().Poison(s.ctx.PID())
}

func (s *ServerSession) parseMessage(msg []byte) error {
	parser := fastjson.Parser{}
	data, err := parser.ParseBytes(msg)
	if err != nil {
		return err
	}

	method := data.GetStringBytes("method")

	switch string(method) {
	case "subscribe":
		if err := s.handleSubscription(data, false); err != nil {
			s.conn.WriteJSON(map[string]any{"error": err.Error()})
		}
	case "unsubscribe":
		if err := s.handleSubscription(data, true); err != nil {
			s.conn.WriteJSON(map[string]any{"error": err.Error()})
		}
	case "getrange":
		data = data.Get("data")
		pair := data.Get("pair")
		s.ctx.Send(s.ctx.PID(), &event.GetRange{
			Stream:    uint32(data.GetUint("stream")),
			From:      data.GetInt64("from"),
			To:        data.GetInt64("to"),
			Timeframe: data.GetInt64("timeframe"),
			Pair: &event.Pair{
				Symbol:   string(pair.GetStringBytes("symbol")),
				Exchange: string(pair.GetStringBytes("exchange")),
			},
		})
	case "getserverconfig":
		clientVersion := string(data.GetStringBytes("version"))
		if err := s.sendServerConfig(); err != nil {
			s.conn.WriteJSON(map[string]any{"error": err.Error()})
			return nil
		}
		slog.Info("sent configuration to client", "session_id", s.id, "client_version", clientVersion)
	default:
		s.conn.WriteJSON(map[string]any{"error": "invalid method"})
	}
	return nil
}

func (s *ServerSession) handleSubscription(data *fastjson.Value, unsub bool) error {
	v := data.Get("data")
	p := v.Get("pair")
	pair := &event.Pair{
		Exchange: string(p.GetStringBytes("exchange")),
		Symbol:   string(p.GetStringBytes("symbol")),
	}
	timeframe := v.GetInt64("timeframe")
	stream := event.Stream(v.GetUint("stream"))
	if !stream.IsValid() {
		return fmt.Errorf("invalid stream")
	}

	subject := nats.Subject{
		StreamType: event.ClientStreamToNatsStream(stream),
		Exchange:   pair.Exchange,
		Symbol:     pair.Symbol,
		Timeframe:  timeframe,
	}

	fmt.Println(subject)

	if unsub {
		if _, ok := s.streams[subject]; !ok {
			return fmt.Errorf("not subscribed to stream")
		}
		s.ctx.Send(s.routerPID, &event.Unsubscribe{
			Subject: subject,
			PID:     s.ctx.PID(),
		})
		delete(s.streams, subject)
		return nil
	}

	if _, ok := s.streams[subject]; ok {
		return fmt.Errorf("already subscribed to stream")
	}
	s.ctx.Send(s.routerPID, &event.Subscribe{
		Subject: subject,
		PID:     s.ctx.PID(),
	})
	s.streams[subject] = true
	return nil

}

func (s *ServerSession) send(p types.WSPayload) {
	b, err := json.Marshal(p)
	if err != nil {
		slog.Error("failed to marshal ws payload", "err", err.Error())
		return
	}
	s.conn.WriteMessage(websocket.BinaryMessage, b)
	metrics.ReportServerWSMessage(s.id.String())
}

func (s *ServerSession) sendServerConfig() error {
	version, err := getLatestClientVersion()
	if err != nil {
		log.Error("failed to retrieve the latest client version", "err", err.Error())
		return err
	}

	cfg := config.Get()
	cfg.Version = version
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	payload := types.WSPayload{
		Stream: event.StreamConfig,
		Data:   data,
	}

	s.send(payload)
	return nil
}

func getLatestClientVersion() (string, error) {
	url := "https://version.marketmonkeyterminal.com/"
	resp, err := http.Get(url)
	if err != nil {
		log.Error("failed to retrieve latest version", "err", err.Error())
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("failed to read and parse response body", "err", err.Error())
		return "", err
	}
	return string(b), nil
}

func (s *ServerSession) getRange(msg *event.GetRange) {
	slog.Info("get range", "pair", msg.Pair.Symbol, "exchange", msg.Pair.Exchange, "stream", msg.Stream, "from", msg.From, "to", msg.To, "timeframe", msg.Timeframe)
	var payload any
	switch event.Stream(msg.Stream) {
	case event.StreamCandles:
		candles, err := s.dbClient.GetCandles(msg.Pair, msg.From, msg.To, msg.Timeframe)
		if err != nil {
			slog.Error("get candle range returned error", "error", err)
			break
		}
		payload = &event.Candles{
			Pair:      msg.Pair,
			Values:    candles,
			Timeframe: msg.Timeframe,
		}
	case event.StreamHeatmaps:
		heatmaps, err := s.dbClient.GetHeatmaps(msg.Pair, msg.From, msg.To, msg.Timeframe)
		if err != nil {
			slog.Error("get heatmap range returned error", "error", err)
			break
		}
		payload = &event.Heatmaps{
			Pair:   msg.Pair,
			Values: heatmaps,
		}
	case event.StreamStats:
		stats, err := s.dbClient.GetStats(msg.Pair, msg.From, msg.To, msg.Timeframe)
		if err != nil {
			slog.Error("get stats range returned error", "error", err)
			break
		}
		payload = &event.Stats{
			Pair:      msg.Pair,
			Timeframe: msg.Timeframe,
			Values:    stats,
		}
	case event.StreamVolumes:
		volumes, err := s.dbClient.GetVolumes(msg.Pair, msg.From, msg.To, msg.Timeframe)
		if err != nil {
			slog.Error("get volume range returned error", "error", err)
			break
		}
		payload = &event.Volumes{
			Pair:      msg.Pair,
			Values:    volumes,
			Timeframe: msg.Timeframe,
		}
	}
	s.ctx.Send(s.ctx.PID(), payload)
}
