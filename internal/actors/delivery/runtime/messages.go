package deliveryruntime

import (
	"github.com/anthdm/hollywood/actor"
	"github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/ids"
)

// RegisterSession registers a session PID in the router.
type RegisterSession struct {
	SessionID ids.SessionID
	PID       *actor.PID
}

// UnregisterSession removes a session and all its subscriptions.
type UnregisterSession struct {
	SessionID ids.SessionID
}

// SubscribeSession binds one session to one subject.
type SubscribeSession struct {
	SessionID ids.SessionID
	Subject   domain.Subject
}

// UnsubscribeSession removes one session-subject binding.
type UnsubscribeSession struct {
	SessionID ids.SessionID
	Subject   domain.Subject
}

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

type busEnvelopeMsg struct {
	Env envelope.Envelope
}
