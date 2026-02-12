// Package domain contains the delivery bounded context domain model.
// A Session represents a connected client. Subscriptions define which
// subjects the session wants to receive.
package domain

import (
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/problem"
)

// Filter holds optional subscription criteria applied server-side.
type Filter struct {
	// MinSpread only delivers updates when spread >= MinSpread (0 = no filter).
	MinSpread float64
}

// Subscription couples a Subject with optional Filters.
type Subscription struct {
	Subject Subject
	Filter  Filter
}

// Session represents a single connected client session.
//
// Invariants:
//   - SessionID is non-empty UUID.
//   - Subscriptions are unique by Subject.
type Session struct {
	id            ids.SessionID
	subscriptions map[Subject]Subscription
}

// NewSession creates a Session with a generated ID.
func NewSession() *Session {
	return &Session{
		id:            ids.NewSessionID(),
		subscriptions: make(map[Subject]Subscription),
	}
}

// ID returns the session's unique identifier.
func (s *Session) ID() ids.SessionID { return s.id }

// Subscribe adds or updates a subscription for the given subject.
func (s *Session) Subscribe(subject Subject, filter Filter) *problem.Problem {
	s.subscriptions[subject] = Subscription{Subject: subject, Filter: filter}
	return nil
}

// Unsubscribe removes a subscription. Returns NotFound if not subscribed.
func (s *Session) Unsubscribe(subject Subject) *problem.Problem {
	if _, ok := s.subscriptions[subject]; !ok {
		return problem.Newf(problem.NotFound, "not subscribed to subject %q", subject.String())
	}
	delete(s.subscriptions, subject)
	return nil
}

// Subscriptions returns a snapshot of all active subscriptions.
func (s *Session) Subscriptions() []Subscription {
	out := make([]Subscription, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		out = append(out, sub)
	}
	return out
}

// IsSubscribed reports whether the session is subscribed to the given subject.
func (s *Session) IsSubscribed(subject Subject) bool {
	_, ok := s.subscriptions[subject]
	return ok
}
