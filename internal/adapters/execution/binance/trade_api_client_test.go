package binance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
)

type staticBroker struct {
	lease            executioncred.TradeCredentialLease
	err              error
	lastRequirement  executiongovernance.CredentialRequirement
	lastObservedAtMs int64
}

func (b *staticBroker) ResolveCredential(requirement executiongovernance.CredentialRequirement, observedAtMs int64) executiongovernance.CredentialResolution {
	b.lastRequirement = requirement
	b.lastObservedAtMs = observedAtMs
	if b.err != nil {
		return executiongovernance.CredentialResolution{
			Availability:  executiongovernance.CredentialAvailabilityUnavailable,
			Status:        executiongovernance.CredentialResolutionDenied,
			EvaluatedAtMs: observedAtMs,
			Requirement:   requirement,
		}
	}
	return executiongovernance.CredentialResolution{
		Availability:  executiongovernance.CredentialAvailabilityAvailable,
		Status:        executiongovernance.CredentialResolutionResolved,
		EvaluatedAtMs: observedAtMs,
		Requirement:   requirement,
		Credential:    b.lease.Credential,
	}
}

func (b *staticBroker) AcquireTradeCredentialLease(requirement executiongovernance.CredentialRequirement, observedAtMs int64) (executioncred.TradeCredentialLease, error) {
	b.lastRequirement = requirement
	b.lastObservedAtMs = observedAtMs
	if b.err != nil {
		return executioncred.TradeCredentialLease{}, b.err
	}
	return b.lease, nil
}

func tradeCredentialRequirement() executiongovernance.CredentialRequirement {
	return executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            "execution.adapter",
		AdapterID:           "binance.spot",
		Mode:                "real_adapter_safe",
		Scope:               executioncred.ScopeTradeOnly,
		TradeOnly:           true,
		AcceptedResolverIDs: []string{executioncred.ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{executioncred.ProviderIDEnvStaticV1},
	}
}

func staticLease() executioncred.TradeCredentialLease {
	req := tradeCredentialRequirement()
	return executioncred.TradeCredentialLease{
		Credential: executiongovernance.ResolvedCredential{
			Boundary:  req.Boundary,
			AdapterID: req.AdapterID,
			Mode:      req.Mode,
			Scope:     req.Scope,
			TradeOnly: true,
			Lease: executiongovernance.CredentialLease{
				LeaseID:      "lease-1",
				State:        executiongovernance.CredentialLeaseStateActive,
				IssuedAtMs:   1_700_000_001_000,
				ValidUntilMs: 1_700_000_031_000,
			},
			Provenance: executiongovernance.CredentialProvenance{
				ResolverID: executioncred.ResolverIDTradeBrokerV1,
				ProviderID: executioncred.ProviderIDEnvStaticV1,
			},
		},
		Material: executioncred.TradeCredentials{
			APIKey:    "api-key",
			APISecret: "secret-key",
		},
	}
}

func TestTradeAPIClient_SubmitTestOrderSuccess(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.String()
		if got := r.Header.Get(binanceTradeAPIHeader); got != "api-key" {
			t.Fatalf("header %s=%q want api-key", binanceTradeAPIHeader, got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	broker := &staticBroker{lease: staticLease()}
	client, err := NewTradeAPIClient(TradeAPIClientConfig{
		BaseURL:               server.URL,
		RecvWindowMs:          3000,
		CredentialRequirement: tradeCredentialRequirement(),
	}, broker)
	if err != nil {
		t.Fatalf("NewTradeAPIClient() error = %v", err)
	}

	venueOrderID, err := client.SubmitTestOrder(context.Background(), TestOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "MARKET",
		Quantity:      1.5,
		ClientOrderID: "client-1",
		TimestampMs:   1700000001000,
	})
	if err != nil {
		t.Fatalf("SubmitTestOrder() error = %v", err)
	}
	if venueOrderID == "" {
		t.Fatal("venueOrderID must not be empty")
	}
	if !strings.Contains(capturedPath, "/api/v3/order/test?") {
		t.Fatalf("captured path=%q want test-order endpoint", capturedPath)
	}
	if !strings.Contains(capturedPath, "signature=") {
		t.Fatalf("captured path=%q missing signature", capturedPath)
	}
	if !strings.Contains(capturedPath, "symbol=BTCUSDT") {
		t.Fatalf("captured path=%q missing symbol", capturedPath)
	}
	if broker.lastRequirement.AdapterID != "binance.spot" {
		t.Fatalf("broker requirement adapter=%q want=binance.spot", broker.lastRequirement.AdapterID)
	}
	if broker.lastObservedAtMs != 1700000001000 {
		t.Fatalf("broker observed_at_ms=%d want=1700000001000", broker.lastObservedAtMs)
	}
}

func TestTradeAPIClient_SubmitTestOrderRejectsOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":-2015,"msg":"Invalid API-key"}`))
	}))
	defer server.Close()

	client, err := NewTradeAPIClient(TradeAPIClientConfig{
		BaseURL:               server.URL,
		CredentialRequirement: tradeCredentialRequirement(),
	}, &staticBroker{lease: staticLease()})
	if err != nil {
		t.Fatalf("NewTradeAPIClient() error = %v", err)
	}

	if _, err := client.SubmitTestOrder(context.Background(), TestOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "MARKET",
		Quantity:      1,
		ClientOrderID: "client-2",
		TimestampMs:   1700000002000,
	}); err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}

func TestTradeAPIClient_SubmitOrderParsesSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s want=POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/api/v3/order") {
			t.Fatalf("path=%q want /api/v3/order", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"symbol":"BTCUSDT",
			"orderId":12345,
			"clientOrderId":"cid-12345",
			"status":"NEW",
			"price":"100.00000000",
			"origQty":"2.00000000",
			"executedQty":"0.00000000",
			"cummulativeQuoteQty":"0.00000000",
			"transactTime":1700000003000
		}`))
	}))
	defer server.Close()

	client, err := NewTradeAPIClient(TradeAPIClientConfig{
		BaseURL:               server.URL,
		RecvWindowMs:          3000,
		CredentialRequirement: tradeCredentialRequirement(),
	}, &staticBroker{lease: staticLease()})
	if err != nil {
		t.Fatalf("NewTradeAPIClient() error = %v", err)
	}

	snapshot, err := client.SubmitOrder(context.Background(), TestOrderRequest{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		OrderType:     "LIMIT",
		TimeInForce:   "GTC",
		Quantity:      2,
		LimitPrice:    100,
		ClientOrderID: "cid-12345",
		TimestampMs:   1700000003000,
	})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if snapshot.Status != "NEW" {
		t.Fatalf("status=%q want=NEW", snapshot.Status)
	}
	if snapshot.VenueOrderID != "12345" {
		t.Fatalf("venue_order_id=%q want=12345", snapshot.VenueOrderID)
	}
	if snapshot.LeavesQty != 2 {
		t.Fatalf("leaves_qty=%v want=2", snapshot.LeavesQty)
	}
}

func TestTradeAPIClient_QueryOrderParsesFilledSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method=%s want=GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"symbol":"BTCUSDT",
			"orderId":12345,
			"clientOrderId":"cid-12345",
			"status":"FILLED",
			"price":"100.00000000",
			"origQty":"2.00000000",
			"executedQty":"2.00000000",
			"cummulativeQuoteQty":"202.00000000",
			"updateTime":1700000003200
		}`))
	}))
	defer server.Close()

	client, err := NewTradeAPIClient(TradeAPIClientConfig{
		BaseURL:               server.URL,
		CredentialRequirement: tradeCredentialRequirement(),
	}, &staticBroker{lease: staticLease()})
	if err != nil {
		t.Fatalf("NewTradeAPIClient() error = %v", err)
	}
	snapshot, err := client.QueryOrder(context.Background(), "BTCUSDT", "12345", "cid-12345", 1700000003200)
	if err != nil {
		t.Fatalf("QueryOrder() error = %v", err)
	}
	if snapshot.Status != "FILLED" {
		t.Fatalf("status=%q want=FILLED", snapshot.Status)
	}
	if snapshot.CumulativeFilledQty != 2 {
		t.Fatalf("cumulative=%v want=2", snapshot.CumulativeFilledQty)
	}
	if snapshot.LeavesQty != 0 {
		t.Fatalf("leaves=%v want=0", snapshot.LeavesQty)
	}
	if snapshot.AvgFillPrice != 101 {
		t.Fatalf("avg_fill_price=%v want=101", snapshot.AvgFillPrice)
	}
}
