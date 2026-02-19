package main

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	mdruntime "github.com/market-raccoon/internal/actors/marketdata/runtime"
	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	"github.com/market-raccoon/internal/adapters/exchange/coinbase"
	"github.com/market-raccoon/internal/adapters/exchange/hyperliquid"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
)

type consumerExchangeRuntime struct {
	Subsystem  actorruntime.Subsystem
	Exchange   config.ConsumerExchangeConfig
	ParseV1    mdruntime.ParseFunc
	ParseV2    mdruntime.ParseFuncV2
	ParseBatch mdruntime.ParseFuncBatch
	ManagerCfg *ws.ManagerConfig
}

func buildExchangeRuntimes(cfg config.AppConfig, logger *slog.Logger) ([]consumerExchangeRuntime, *problem.Problem) {
	exchanges := configuredExchanges(cfg)
	if len(exchanges) == 0 {
		return nil, problem.New(problem.ValidationFailed, "consumer: no exchange configured")
	}

	multi := len(exchanges) > 1
	out := make([]consumerExchangeRuntime, 0, len(exchanges))
	for _, exchange := range exchanges {
		ex := exchange
		if p := validateRuntimeExchange(ex); p != nil {
			return nil, p
		}
		if strings.TrimSpace(ex.MarketType) == "" {
			ex.MarketType = defaultMarketTypeForExchange(ex.Type)
		}
		runtimeCfg, p := buildExchangeRuntime(cfg, logger, ex, multi)
		if p != nil {
			return nil, p
		}
		out = append(out, runtimeCfg)
	}

	return out, nil
}

func configuredExchanges(cfg config.AppConfig) []config.ConsumerExchangeConfig {
	exchanges := cfg.Consumer.Exchanges
	if len(exchanges) != 0 {
		return exchanges
	}
	exchange := strings.ToLower(strings.TrimSpace(cfg.Consumer.Exchange))
	if exchange == "" {
		exchange = "binance"
	}
	return []config.ConsumerExchangeConfig{
		{
			Name:       exchange,
			Type:       exchange,
			BaseURL:    strings.TrimSpace(cfg.Consumer.BinanceWSBaseURL),
			Tickers:    append([]string(nil), cfg.Consumer.Tickers...),
			MarketType: strings.ToUpper(strings.TrimSpace(cfg.Consumer.MarketType)),
		},
	}
}

func validateRuntimeExchange(ex config.ConsumerExchangeConfig) *problem.Problem {
	if strings.TrimSpace(ex.Name) == "" {
		return problem.New(problem.ValidationFailed, "consumer: exchange.name must not be empty")
	}
	if len(ex.Tickers) == 0 {
		return problem.Newf(problem.ValidationFailed, "consumer: exchange %q has no tickers", ex.Name)
	}
	return nil
}

func buildExchangeRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	multi bool,
) (consumerExchangeRuntime, *problem.Problem) {
	subsystem := marketDataSubsystemKey(ex.Name, multi)
	switch strings.ToLower(strings.TrimSpace(ex.Type)) {
	case "binance":
		return buildBinanceRuntime(cfg, logger, ex, subsystem), nil
	case "bybit":
		return buildBybitRuntime(cfg, logger, ex, subsystem), nil
	case "coinbase":
		return buildCoinbaseRuntime(cfg, logger, ex, subsystem), nil
	case "hyperliquid":
		return buildHyperLiquidRuntime(cfg, logger, ex, subsystem), nil
	default:
		return consumerExchangeRuntime{}, problem.Newf(problem.ValidationFailed, "consumer: unsupported exchange type %q", ex.Type)
	}
}

func buildBinanceRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	isFutures := strings.Contains(strings.ToUpper(strings.TrimSpace(ex.MarketType)), "FUTURES")
	enableExtras := cfg.Consumer.EnableMarkPriceLiquidation || isFutures
	if enableExtras {
		managerCfg.StreamsPerTicker = 4
	} else {
		managerCfg.StreamsPerTicker = 2
	}
	if isFutures && strings.TrimSpace(ex.BaseURL) == "" {
		ex.BaseURL = binance.DefaultFuturesWSBaseURL
	}
	managerCfg.SubscriptionBuilder = func([]string) [][]byte { return nil }
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint, p := binance.BuildEndpoint(ex.BaseURL, bucket, enableExtras)
		if p != nil {
			logger.Error("consumer: binance endpoint build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return ""
		}
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := binance.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
		if p != nil {
			logger.Warn("consumer: binance parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := binance.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: binance parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ManagerCfg: &managerCfg,
	}
}

func buildCoinbaseRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	managerCfg.StreamsPerTicker = 3
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint := coinbase.BuildEndpoint(ex.BaseURL)
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}
	managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
		msgs, p := coinbase.BuildSubscriptions(bucket)
		if p != nil {
			logger.Error("consumer: coinbase subscription build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return nil
		}
		return msgs
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := coinbase.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
		if p != nil {
			logger.Warn("consumer: coinbase parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := coinbase.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: coinbase parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ManagerCfg: &managerCfg,
	}
}

func buildHyperLiquidRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	managerCfg.StreamsPerTicker = 2
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint := hyperliquid.BuildEndpoint(ex.BaseURL)
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}
	managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
		msgs, p := hyperliquid.BuildSubscriptionsWithMarkPrice(bucket)
		if p != nil {
			logger.Error("consumer: hyperliquid subscription build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return nil
		}
		return msgs
	}

	// Build subscribed coin set for allMids filtering.
	subscribedCoins := make(map[string]bool, len(ex.Tickers))
	for _, t := range ex.Tickers {
		coin := hyperliquid.ToCoinName(t)
		if coin != "" {
			subscribedCoins[strings.ToUpper(coin)] = true
		}
	}
	batchParser := hyperliquid.ParseAllMids(subscribedCoins, ex.MarketType)
	parseBatch := func(msg *ws.WsMessage) ([]mdapp.IngestRequest, error) {
		reqs, err := batchParser(msg.Data, msg.RecvAt)
		if err != nil {
			return nil, err
		}
		if reqs == nil {
			return nil, nil
		}
		for i := range reqs {
			enrichRequestMetadata(&reqs[i], msg, ex.MarketType, "allMids")
		}
		return reqs, nil
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := hyperliquid.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
		if p != nil {
			logger.Warn("consumer: hyperliquid parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := hyperliquid.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: hyperliquid parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ParseBatch: parseBatch,
		ManagerCfg: &managerCfg,
	}
}

func buildBybitRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	if cfg.Consumer.EnableMarkPriceLiquidation {
		managerCfg.StreamsPerTicker = 4
	} else {
		managerCfg.StreamsPerTicker = 2
	}
	managerCfg.Heartbeat = func() ws.Heartbeat {
		return ws.Heartbeat{
			Interval: 20 * time.Second,
			Message:  []byte(`{"op":"ping"}`),
		}
	}
	managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
		msgs, p := bybit.BuildSubscriptions(bucket, cfg.Consumer.EnableMarkPriceLiquidation)
		if p != nil {
			logger.Error("consumer: bybit subscription build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return nil
		}
		return msgs
	}
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint, p := bybit.BuildEndpoint(ex.BaseURL, bucket, ex.MarketType)
		if p != nil {
			logger.Error("consumer: bybit endpoint build failed", "err", p, "exchange", ex.Name, "bucket", bucket)
			return ""
		}
		logger.Info("consumer: ws endpoint planned", "exchange", ex.Name, "endpoint", endpoint, "bucket", bucket)
		return endpoint
	}

	parseV1 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool) {
		req, skip, p := bybit.ParseMessageForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, "")
		if p != nil {
			logger.Warn("consumer: bybit parse skipped message",
				"code", p.Code,
				"message", p.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
		}
		return req, skip || p != nil
	}
	parseV2 := func(msg *ws.WsMessage) (mdapp.IngestRequest, bool, mdruntime.ParseMeta) {
		req, skip, meta := bybit.ParseMessageWithMetaForMarketType(msg.Data, msg.RecvAt, ex.MarketType)
		enrichRequestMetadata(&req, msg, ex.MarketType, meta.WSStream)
		outMeta := toRuntimeParseMeta(meta.EventType, meta.SkipReason, meta.WSStream, meta.Ticker, meta.Problem)
		if meta.Problem != nil {
			logger.Warn("consumer: bybit parse skipped message",
				"code", meta.Problem.Code,
				"message", meta.Problem.Message,
				"exchange", msg.Exchange,
				"endpoint", msg.Endpoint,
				"bucket_id", msg.BucketID,
			)
			return req, true, outMeta
		}
		return req, skip, outMeta
	}
	return consumerExchangeRuntime{
		Subsystem:  subsystem,
		Exchange:   ex,
		ParseV1:    parseV1,
		ParseV2:    parseV2,
		ManagerCfg: &managerCfg,
	}
}

func defaultMarketTypeForExchange(exchangeType string) string {
	switch strings.ToLower(strings.TrimSpace(exchangeType)) {
	case "hyperliquid":
		return domain.MarketTypeUSDMFutures.String()
	default:
		return domain.MarketTypeSpot.String()
	}
}

func baseManagerConfig(cfg config.AppConfig, ex config.ConsumerExchangeConfig) ws.ManagerConfig {
	return ws.ManagerConfig{
		Exchange:               ex.Name,
		Tickers:                ex.Tickers,
		StreamsPerTicker:       cfg.Consumer.StreamsPerTicker,
		MaxStreamsPerWebsocket: cfg.Consumer.MaxStreamsPerWebsocket,
		FillStrategy:           ws.FillStrategyAuto,
		MaxWebsockets:          cfg.Consumer.MaxWebsockets,
		MaxWebsocketLifetime:   cfg.Consumer.MaxWebsocketLifetimeDuration(),
		RespawnOverlap:         cfg.Consumer.RespawnOverlapDuration(),
		Heartbeat:              func() ws.Heartbeat { return ws.Heartbeat{} },
		Reconnect: ws.ReconnectPolicy{
			BaseBackoff:  cfg.Consumer.ReconnectBaseBackoffDuration(),
			MaxBackoff:   cfg.Consumer.ReconnectMaxBackoffDuration(),
			Jitter:       cfg.Consumer.ReconnectJitter,
			RetryBudget:  cfg.Consumer.ReconnectRetryBudget,
			BudgetWindow: cfg.Consumer.ReconnectBudgetWindowDuration(),
			Cooldown:     cfg.Consumer.ReconnectCooldownDuration(),
		},
	}
}

func marketDataSubsystemKey(exchangeName string, multi bool) actorruntime.Subsystem {
	if !multi {
		return actorruntime.SubsystemMarketData
	}
	name := strings.ToLower(strings.TrimSpace(exchangeName))
	if name == "" {
		name = "exchange"
	}
	return actorruntime.Subsystem("marketdata:" + name)
}

func enrichRequestMetadata(req *mdapp.IngestRequest, msg *ws.WsMessage, defaultMarketType, wsStream string) {
	if req.Metadata == nil {
		req.Metadata = make(map[string]string, 8)
	}
	if strings.TrimSpace(req.MarketType) == "" {
		req.MarketType = strings.ToUpper(strings.TrimSpace(defaultMarketType))
		if req.MarketType == "" {
			req.MarketType = "SPOT"
		}
	}
	if req.Metadata["instrument_market_type"] == "" {
		req.Metadata["instrument_market_type"] = req.MarketType
	}
	req.Metadata["exchange"] = msg.Exchange
	req.Metadata["endpoint"] = msg.Endpoint
	req.Metadata["bucket_id"] = fmt.Sprintf("%d", msg.BucketID)
	req.Metadata["consumer_id"] = msg.ConsumerID
	req.Metadata["recv_at"] = fmt.Sprintf("%d", msg.RecvAt.UnixMilli())
	if wsStream != "" {
		req.Metadata["ws_stream"] = wsStream
	}
}

func toRuntimeParseMeta(eventType, skipReason, wsStream, ticker string, p *problem.Problem) mdruntime.ParseMeta {
	out := mdruntime.ParseMeta{
		EventType:  eventType,
		SkipReason: skipReason,
		WSStream:   wsStream,
		Ticker:     ticker,
	}
	if p != nil {
		out.ProblemCode = string(p.Code)
		out.ProblemMessage = p.Message
	}
	return out
}
