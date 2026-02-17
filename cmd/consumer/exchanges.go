package main

import (
	"fmt"
	"log/slog"
	"strings"

	mdruntime "github.com/market-raccoon/internal/actors/marketdata/runtime"
	ws "github.com/market-raccoon/internal/actors/marketdata/ws"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
)

type consumerExchangeRuntime struct {
	Subsystem  actorruntime.Subsystem
	Exchange   config.ConsumerExchangeConfig
	ParseV1    mdruntime.ParseFunc
	ParseV2    mdruntime.ParseFuncV2
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
			ex.MarketType = "SPOT"
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
	managerCfg.SubscriptionBuilder = func([]string) [][]byte { return nil }
	managerCfg.EndpointBuilder = func(bucket []string) string {
		endpoint, p := binance.BuildEndpoint(ex.BaseURL, bucket)
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

func buildBybitRuntime(
	cfg config.AppConfig,
	logger *slog.Logger,
	ex config.ConsumerExchangeConfig,
	subsystem actorruntime.Subsystem,
) consumerExchangeRuntime {
	managerCfg := baseManagerConfig(cfg, ex)
	managerCfg.SubscriptionBuilder = func(bucket []string) [][]byte {
		msgs, p := bybit.BuildSubscriptions(bucket)
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
