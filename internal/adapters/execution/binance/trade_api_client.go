package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

const (
	defaultTradeAPIBaseURL    = "https://testnet.binance.vision"
	defaultTradeAPIRecvWindow = int64(5000)
	defaultTradeAPITimeout    = 3 * time.Second
	binanceTradeAPIHeader     = "X-MBX-APIKEY"
	binancePathTestOrder      = "/api/v3/order/test"
	binancePathOrder          = "/api/v3/order"
)

var (
	// ErrTradeCredentialsUnavailable indicates missing or invalid trade credentials.
	ErrTradeCredentialsUnavailable = fmt.Errorf("trade credentials unavailable")
	// ErrTradeOnlyScopeRequired indicates credentials without trade-only semantics.
	ErrTradeOnlyScopeRequired = fmt.Errorf("trade-only credential scope required")
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// TradeAPIClientConfig configures signed Binance trade API requests.
type TradeAPIClientConfig struct {
	BaseURL               string
	RecvWindowMs          int64
	RequestTimeout        time.Duration
	CredentialRequirement executiongovernance.CredentialRequirement
}

// TestOrderRequest is the request payload sent to Binance `/api/v3/order/test`.
type TestOrderRequest struct {
	Symbol        string
	Side          string
	OrderType     string
	TimeInForce   string
	Quantity      float64
	LimitPrice    float64
	ClientOrderID string
	TimestampMs   int64
}

// OrderSnapshot captures normalized order status observed from Binance.
type OrderSnapshot struct {
	VenueOrderID        string
	ClientOrderID       string
	Status              string
	RequestedQty        float64
	CumulativeFilledQty float64
	LeavesQty           float64
	LimitPrice          float64
	AvgFillPrice        float64
	LastFillPrice       float64
	TsExchangeMs        int64
}

type binanceOrderResponse struct {
	OrderID            int64  `json:"orderId"`
	ClientOrderID      string `json:"clientOrderId"`
	Status             string `json:"status"`
	Price              string `json:"price"`
	OrigQty            string `json:"origQty"`
	ExecutedQty        string `json:"executedQty"`
	CumulativeQuoteQty string `json:"cummulativeQuoteQty"`
	UpdateTime         int64  `json:"updateTime"`
	TransactTime       int64  `json:"transactTime"`
}

// TradeAPIClient performs signed trade-only calls to Binance test-order endpoint.
type TradeAPIClient struct {
	baseURL               string
	recvWindowMs          int64
	timeout               time.Duration
	httpClient            httpDoer
	broker                executioncred.Broker
	credentialRequirement executiongovernance.CredentialRequirement
}

func NewTradeAPIClient(cfg TradeAPIClientConfig, broker executioncred.Broker) (*TradeAPIClient, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		baseURL = defaultTradeAPIBaseURL
	}
	recvWindow := cfg.RecvWindowMs
	if recvWindow <= 0 {
		recvWindow = defaultTradeAPIRecvWindow
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = defaultTradeAPITimeout
	}
	if broker == nil {
		return nil, fmt.Errorf("%w: broker is nil", ErrTradeCredentialsUnavailable)
	}
	return &TradeAPIClient{
		baseURL:               strings.TrimRight(baseURL, "/"),
		recvWindowMs:          recvWindow,
		timeout:               timeout,
		httpClient:            &http.Client{Timeout: timeout},
		broker:                broker,
		credentialRequirement: cfg.CredentialRequirement,
	}, nil
}

// SubmitTestOrder sends a signed request to Binance test order endpoint.
func (c *TradeAPIClient) SubmitTestOrder(ctx context.Context, req TestOrderRequest) (string, error) {
	if c == nil {
		return "", fmt.Errorf("trade api client is nil")
	}
	query, timestampMs := c.buildOrderQuery(req)
	if _, err := c.signedRequest(ctx, http.MethodPost, binancePathTestOrder, query); err != nil {
		return "", err
	}

	venueOrderID := "BN-TEST-" + strings.ToUpper(sharedhash.HashFieldsFast("binance-test-order", req.ClientOrderID, strconv.FormatInt(timestampMs, 10)))
	if len(venueOrderID) > 36 {
		venueOrderID = venueOrderID[:36]
	}
	return venueOrderID, nil
}

// SubmitOrder sends a signed request to Binance create-order endpoint.
func (c *TradeAPIClient) SubmitOrder(ctx context.Context, req TestOrderRequest) (OrderSnapshot, error) {
	if c == nil {
		return OrderSnapshot{}, fmt.Errorf("trade api client is nil")
	}
	query, fallbackTs := c.buildOrderQuery(req)
	body, err := c.signedRequest(ctx, http.MethodPost, binancePathOrder, query)
	if err != nil {
		return OrderSnapshot{}, err
	}
	var raw binanceOrderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return OrderSnapshot{}, fmt.Errorf("decode order response: %w", err)
	}
	return normalizeOrderSnapshot(raw, req, fallbackTs), nil
}

// QueryOrder sends a signed request to Binance order-query endpoint.
func (c *TradeAPIClient) QueryOrder(ctx context.Context, symbol, venueOrderID, clientOrderID string, timestampMs int64) (OrderSnapshot, error) {
	if c == nil {
		return OrderSnapshot{}, fmt.Errorf("trade api client is nil")
	}
	if timestampMs <= 0 {
		timestampMs = time.Now().UnixMilli()
	}
	query := url.Values{}
	query.Set("symbol", strings.ToUpper(strings.TrimSpace(symbol)))
	query.Set("recvWindow", strconv.FormatInt(c.recvWindowMs, 10))
	query.Set("timestamp", strconv.FormatInt(timestampMs, 10))
	if strings.TrimSpace(venueOrderID) != "" {
		query.Set("orderId", strings.TrimSpace(venueOrderID))
	}
	if strings.TrimSpace(clientOrderID) != "" {
		query.Set("origClientOrderId", strings.TrimSpace(clientOrderID))
	}
	body, err := c.signedRequest(ctx, http.MethodGet, binancePathOrder, query)
	if err != nil {
		return OrderSnapshot{}, err
	}
	var raw binanceOrderResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return OrderSnapshot{}, fmt.Errorf("decode order query response: %w", err)
	}
	return normalizeOrderSnapshot(raw, TestOrderRequest{
		Symbol:        symbol,
		ClientOrderID: clientOrderID,
		TimestampMs:   timestampMs,
	}, timestampMs), nil
}

func (c *TradeAPIClient) buildOrderQuery(req TestOrderRequest) (url.Values, int64) {
	timestampMs := req.TimestampMs
	if timestampMs <= 0 {
		timestampMs = time.Now().UnixMilli()
	}
	query := url.Values{}
	query.Set("symbol", strings.ToUpper(strings.TrimSpace(req.Symbol)))
	query.Set("side", strings.ToUpper(strings.TrimSpace(req.Side)))
	query.Set("type", strings.ToUpper(strings.TrimSpace(req.OrderType)))
	query.Set("quantity", formatDecimal(req.Quantity))
	query.Set("newClientOrderId", strings.TrimSpace(req.ClientOrderID))
	query.Set("recvWindow", strconv.FormatInt(c.recvWindowMs, 10))
	query.Set("timestamp", strconv.FormatInt(timestampMs, 10))
	if strings.EqualFold(strings.TrimSpace(req.OrderType), "LIMIT") {
		query.Set("timeInForce", strings.ToUpper(strings.TrimSpace(req.TimeInForce)))
		query.Set("price", formatDecimal(req.LimitPrice))
	}
	return query, timestampMs
}

func (c *TradeAPIClient) signedRequest(ctx context.Context, method, path string, query url.Values) ([]byte, error) {
	lease, err := c.broker.AcquireTradeCredentialLease(c.credentialRequirement, parseTimestamp(query))
	if err != nil {
		return nil, err
	}
	if !lease.Credential.TradeOnly || !strings.EqualFold(strings.TrimSpace(lease.Credential.Scope), executioncred.ScopeTradeOnly) {
		return nil, ErrTradeOnlyScopeRequired
	}
	encoded := query.Encode()
	signature := signQuery(encoded, lease.Material.APISecret)
	targetURL := c.baseURL + path + "?" + encoded + "&signature=" + signature

	requestCtx := ctx
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		requestCtx, cancel = context.WithTimeout(requestCtx, c.timeout)
		defer cancel()
	}

	httpReq, err := http.NewRequestWithContext(requestCtx, method, targetURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build trade api request: %w", err)
	}
	httpReq.Header.Set(binanceTradeAPIHeader, lease.Material.APIKey)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("trade api call failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("trade api rejected request: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

//nolint:gocyclo // Explicit normalization keeps venue-to-domain translation deterministic and local.
func normalizeOrderSnapshot(raw binanceOrderResponse, req TestOrderRequest, fallbackTs int64) OrderSnapshot {
	requested := parseDecimal(raw.OrigQty)
	if requested <= 0 {
		requested = math.Abs(req.Quantity)
	}
	cumulative := parseDecimal(raw.ExecutedQty)
	if cumulative < 0 {
		cumulative = 0
	}
	if cumulative > requested && requested > 0 {
		cumulative = requested
	}
	leaves := 0.0
	if requested > cumulative {
		leaves = requested - cumulative
	}
	limitPrice := parseDecimal(raw.Price)
	if limitPrice <= 0 {
		limitPrice = math.Max(req.LimitPrice, 0)
	}
	avgFillPrice := 0.0
	cumulativeQuote := parseDecimal(raw.CumulativeQuoteQty)
	if cumulative > 0 && cumulativeQuote > 0 {
		avgFillPrice = cumulativeQuote / cumulative
	}
	if avgFillPrice <= 0 {
		avgFillPrice = limitPrice
	}
	venueOrderID := strings.TrimSpace(strconv.FormatInt(raw.OrderID, 10))
	if venueOrderID == "0" || venueOrderID == "" {
		venueOrderID = "BN-UNKNOWN-" + strings.ToUpper(sharedhash.HashFieldsFast("binance-order-fallback", req.ClientOrderID, strconv.FormatInt(fallbackTs, 10)))
		if len(venueOrderID) > 36 {
			venueOrderID = venueOrderID[:36]
		}
	}
	clientOrderID := strings.TrimSpace(raw.ClientOrderID)
	if clientOrderID == "" {
		clientOrderID = strings.TrimSpace(req.ClientOrderID)
	}
	ts := raw.UpdateTime
	if ts <= 0 {
		ts = raw.TransactTime
	}
	if ts <= 0 {
		ts = fallbackTs
	}
	return OrderSnapshot{
		VenueOrderID:        venueOrderID,
		ClientOrderID:       clientOrderID,
		Status:              strings.ToUpper(strings.TrimSpace(raw.Status)),
		RequestedQty:        requested,
		CumulativeFilledQty: cumulative,
		LeavesQty:           leaves,
		LimitPrice:          limitPrice,
		AvgFillPrice:        avgFillPrice,
		LastFillPrice:       avgFillPrice,
		TsExchangeMs:        ts,
	}
}

func parseDecimal(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func parseTimestamp(query url.Values) int64 {
	if query == nil {
		return 0
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(query.Get("timestamp")), 10, 64)
	if err != nil {
		return 0
	}
	return ts
}

func signQuery(query string, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}

func formatDecimal(v float64) string {
	if v < 0 {
		v = -v
	}
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}
