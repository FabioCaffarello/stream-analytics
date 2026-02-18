package deliveryruntime

import (
	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/ids"
)

// AttachConn binds a websocket connection to one session actor.
type AttachConn struct {
	Conn wsConn
}

// Subscribe binds one session to one subject.
type Subscribe struct {
	SessionID ids.SessionID
	Subject   domain.Subject
}

// Unsubscribe removes one session-subject binding.
type Unsubscribe struct {
	SessionID ids.SessionID
	Subject   domain.Subject
}

// DeliverEnvelope forwards one raw envelope into routing fan-out.
type DeliverEnvelope struct {
	Envelope envelope.Envelope
}

// SpawnSession asks delivery subsystem to spawn one session actor child.
type SpawnSession struct {
	Config SessionConfig
}

// SpawnSessionAck is returned by delivery subsystem for SpawnSession.
type SpawnSessionAck struct {
	PID *actor.PID
}

type GetRangeRequest struct {
	RequestID string
	Subject   string
	FromMs    int64
	ToMs      int64
	Limit     int
	Page      int
}

type GetRangeResponse struct {
	RequestID string
	Subject   string
	Page      int
	Limit     int
	Items     []ports.RangeItem
}

// RegisterSession registers a session PID in the router.
type RegisterSession struct {
	SessionID ids.SessionID
	PID       *actor.PID
}

// UnregisterSession removes a session and all its subscriptions.
type UnregisterSession struct {
	SessionID ids.SessionID
}

// SubscribeSession is a backward-compatible alias for Subscribe.
type SubscribeSession = Subscribe

// UnsubscribeSession is a backward-compatible alias for Unsubscribe.
type UnsubscribeSession = Unsubscribe

// DeliveryEvent is sent by router to session actors.
type DeliveryEvent struct {
	Subject domain.Subject
	Env     envelope.Envelope
}

// sessionInboundText is emitted by ws read loop into session mailbox.
type sessionInboundText struct {
	Data []byte
}

// sessionDisconnected is emitted by ws read loop on connection close/error.
type sessionDisconnected struct{}

type sessionFlushOutbound struct{}

// busEnvelopeMsg is a legacy local message used by the router consume loop.
type busEnvelopeMsg struct {
	Env envelope.Envelope
}
