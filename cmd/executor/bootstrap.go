package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/anthdm/hollywood/actor"
	executionruntime "github.com/market-raccoon/internal/actors/execution/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	executionadapterbinance "github.com/market-raccoon/internal/adapters/execution/binance"
	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	executionapp "github.com/market-raccoon/internal/core/execution/app"
	executiongovernance "github.com/market-raccoon/internal/core/execution/governance"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	defaultExecutorQueueCapacity = 1024
	executorServiceName          = "executor"
)

type executorEnvelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
}

func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	publisher, closePublisher, err := buildExecutorPublisher(cfg, logger)
	if err != nil {
		return err
	}

	source, err := initExecutorEnvelopeSource(cfg, logger)
	if err != nil {
		_ = closePublisher(context.Background())
		return err
	}

	controlPlane := executionapp.NewInMemoryControlPlane()

	intentExecutor, err := buildIntentExecutor(cfg, logger, controlPlane)
	if err != nil {
		source.shutdownFn(context.Background())
		_ = closePublisher(context.Background())
		return err
	}

	execCfg := executionruntime.SubsystemConfig{
		Logger:       logger.With("subsystem", "execution"),
		EnvelopeCh:   source.envelopeCh,
		Executor:     intentExecutor,
		Publisher:    publisher,
		ReplicaID:    cfg.Shard.Index,
		ReplicaCount: cfg.Shard.Count,
	}
	boundary := execCfg.Executor.BoundaryInfo()
	logger.Info("executor: execution boundary configured",
		"boundary", boundary.Boundary,
		"adapter", boundary.Adapter,
		"mode", boundary.Mode,
	)

	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		source.shutdownFn(context.Background())
		_ = closePublisher(context.Background())
		return err
	}

	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger: logger,
		Factories: map[actorruntime.Subsystem]actor.Producer{
			actorruntime.SubsystemExecution: executionruntime.NewSubsystemActor(execCfg),
		},
	})
	logger.Info("executor: guardian spawned", "pid", guardianPID.String())

	var ready atomic.Bool
	ready.Store(true)

	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
		httpserver.WithReloadHook(protoRolloutReloadHook(configPath, logger)),
		httpserver.WithControlPlane(controlPlane),
	)
	srv.SetReadyGate(func() bool { return ready.Load() })

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := bootstrap.SignalChannel()
	select {
	case sig := <-quit:
		logger.Info("executor: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("executor: HTTP server error", "err", err)
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("executor: consume loop failed", "err", p)
		}
	case <-ctx.Done():
		logger.Info("executor: context canceled")
	}

	ready.Store(false)
	logger.Info("executor: shutting down")

	httpShutCtx, httpCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer httpCancel()
	if err := srv.Shutdown(httpShutCtx); err != nil {
		logger.Warn("executor: HTTP shutdown error", "err", err)
	}

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	source.shutdownFn(depsCtx)
	if p := closePublisher(depsCtx); p != nil {
		logger.Warn("executor: publisher close failed", "err", p)
	}
	actorruntime.ShutdownGuardian(depsCtx, e, guardianPID, logger)
	logger.Info("executor: shutdown complete")
	return nil
}

func buildExecutorPublisher(cfg config.AppConfig, logger *slog.Logger) (executionruntime.EventPublisher, func(context.Context) *problem.Problem, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		pub, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
			URL:            cfg.JetStream.URL,
			StreamName:     cfg.JetStream.StreamName,
			DedupWindow:    cfg.JetStream.DedupWindowDuration(),
			MaxAge:         cfg.JetStream.MaxAgeDuration(),
			MaxBytes:       cfg.JetStream.MaxBytesInt64(),
			PublishTimeout: cfg.Processor.PublisherTimeoutDuration(),
		}, metrics.NewBusObserver())
		if p != nil {
			return nil, nil, fmt.Errorf("executor: jetstream publisher init failed: %v", p)
		}
		return pub, pub.Close, nil
	}

	logPub := bus.NewLogPublisher(logger)
	return logPub, func(context.Context) *problem.Problem { return nil }, nil
}

func initExecutorEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) (executorEnvelopeSource, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		logger.Info("executor: bus.type is not jetstream, no inbound stream configured")
		return executorEnvelopeSource{
			envelopeCh: make(chan envelope.Envelope),
			consumeErr: make(chan *problem.Problem),
			shutdownFn: func(context.Context) {},
		}, nil
	}

	queueCap := cfg.Processor.BusCapacity
	if queueCap <= 0 {
		queueCap = defaultExecutorQueueCapacity
	}
	filters := effectiveExecutorFilters(cfg.JetStream.FilterSubjects)
	durable := serviceDurable(cfg.JetStream.ConsumerDurable, executorServiceName, cfg.Shard.Index, cfg.Shard.Count)

	consumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
		URL:             cfg.JetStream.URL,
		StreamName:      cfg.JetStream.StreamName,
		DedupWindow:     cfg.JetStream.DedupWindowDuration(),
		MaxAge:          cfg.JetStream.MaxAgeDuration(),
		MaxBytes:        cfg.JetStream.MaxBytesInt64(),
		ConsumerDurable: durable,
		FilterSubjects:  filters,
		AckWait:         cfg.JetStream.AckWaitDuration(),
		MaxAckPending:   cfg.JetStream.MaxAckPending,
		MaxDeliver:      cfg.JetStream.MaxDeliver,
		DeliverPolicy:   cfg.JetStream.DeliverPolicy,
		ShardGroupCount: cfg.JetStream.ShardGroupCount,
		ShardGroupID:    cfg.JetStream.ShardGroupID,
		MaxLag:          cfg.Shard.MaxLag,
	}, metrics.NewBusObserver())
	if p != nil {
		return executorEnvelopeSource{}, fmt.Errorf("executor: jetstream consumer init failed: %v", p)
	}

	envCh := make(chan envelope.Envelope, queueCap)
	errCh := make(chan *problem.Problem, 1)
	runCtx, cancel := context.WithCancel(context.Background())

	go func() {
		errCh <- consumer.Consume(runCtx, func(consumeCtx context.Context, env envelope.Envelope) *problem.Problem {
			select {
			case envCh <- env:
				return nil
			case <-consumeCtx.Done():
				return problem.WithRetryable(problem.Wrap(consumeCtx.Err(), problem.Unavailable, "executor enqueue canceled"))
			default:
				return problem.WithRetryable(problem.New(problem.Unavailable, "executor queue full"))
			}
		})
	}()

	logger.Info("executor: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", durable,
		"filters", filters,
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
	)

	return executorEnvelopeSource{
		envelopeCh: envCh,
		consumeErr: errCh,
		shutdownFn: func(shutCtx context.Context) {
			cancel()
			if p := consumer.Close(shutCtx); p != nil {
				logger.Warn("executor: jetstream consumer close failed", "err", p)
			}
		},
	}, nil
}

func effectiveExecutorFilters(base []string) []string {
	const (
		canonicalFamily = "strategy.intent"
		canonicalProbe  = "strategy.intent.v1.binance.BTCUSDT"
	)
	out := make([]string, 0, len(base)+1)
	for _, raw := range base {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if subjectMatchesFilter(canonicalProbe, subject) && filterTargetsEventFamily(subject, canonicalFamily) {
			out = append(out, subject)
		}
	}
	out = appendFilterSubjectIfMissing(out, canonicalFamily+".>")
	return dedupeSubjects(out)
}

func dedupeSubjects(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out
}

func appendFilterSubjectIfMissing(base []string, subject string) []string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return base
	}
	for _, existing := range base {
		if subjectMatchesFilter(subject, strings.TrimSpace(existing)) {
			return base
		}
	}
	return append(base, subject)
}

func subjectMatchesFilter(subject, filter string) bool {
	subject = strings.TrimSpace(subject)
	filter = strings.TrimSpace(filter)
	if subject == "" || filter == "" {
		return false
	}
	if filter == ">" || subject == filter {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ">")
		return strings.HasPrefix(subject, prefix)
	}
	return false
}

func filterTargetsEventFamily(filter, family string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	family = strings.ToLower(strings.TrimSpace(family))
	if filter == "" || family == "" {
		return false
	}
	if filter == family || strings.HasPrefix(filter, family+".") {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ".>")
		return prefix == family || strings.HasPrefix(prefix, family+".")
	}
	return false
}

func serviceDurable(base, service string, index, count int) string {
	base = strings.TrimSpace(base)
	service = strings.TrimSpace(service)
	if base == "" {
		base = service + "-v1"
	} else if service != "" && !strings.Contains(base, "-"+service) {
		base = base + "-" + service
	}
	if count > 1 {
		return fmt.Sprintf("%s-s%d", base, index)
	}
	return base
}

func protoRolloutReloadHook(configPath string, logger *slog.Logger) func() error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	return func() error {
		cfg, prob := config.Load(configPath)
		if prob != nil {
			return fmt.Errorf("reload config load failed: %v", prob)
		}
		if prob := cfg.Validate(); prob != nil {
			return fmt.Errorf("reload config validation failed: %v", prob)
		}
		contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
		logger.Info("executor: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}

func buildIntentExecutor(cfg config.AppConfig, logger *slog.Logger, controlPlane executionports.ControlPlane) (executionports.IntentExecutor, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Execution.Mode))
	switch mode {
	case "", "bootstrap_simulated":
		grant := buildExecutionGrant(cfg)
		adapter := selectBootstrapAdapter(cfg, grant)
		exec := executionapp.NewGovernedExecutor(executionapp.GovernedExecutorConfig{
			Governance: executionapp.NewStaticExecutionGovernance(executionapp.StaticExecutionGovernanceConfig{
				Authorizer: executionapp.StaticCapabilityAuthorizer{Grant: &grant},
				Selector: executionapp.NewStaticAdapterSelector(executionapp.AdapterRoute{
					Boundary:  grant.Boundary,
					AdapterID: grant.AdapterID,
					Mode:      grant.Mode,
				}),
				BoundaryInfo: executionports.BoundaryInfo{
					Boundary: grant.Boundary,
					Adapter:  grant.AdapterID,
					Mode:     grant.Mode,
				},
			}),
			Adapters: map[string]executionports.IntentExecutor{
				grant.AdapterID: adapter,
			},
			ControlPlane: controlPlane,
		})
		logger.Info("executor: using bootstrap simulated adapter",
			"mode", modeOrDefault(mode, "bootstrap_simulated"),
			"adapter", grant.AdapterID,
			"governance_boundary", grant.Boundary,
			"governance_grant", grant.GrantID,
		)
		return exec, nil
	case "real_adapter_safe":
		if !cfg.Execution.Real.Enabled {
			return nil, fmt.Errorf("executor: execution.real.enabled must be true when execution.mode=real_adapter_safe")
		}
		if strings.ToLower(strings.TrimSpace(cfg.Execution.Adapter)) != "binance.spot" {
			return nil, fmt.Errorf("executor: execution.adapter must be binance.spot when execution.mode=real_adapter_safe")
		}
		credentialProvider := executioncred.NewEnvProvider(executioncred.EnvProviderConfig{
			APIKeyEnv:    cfg.Execution.Real.Binance.TradeAPI.APIKeyEnv,
			APISecretEnv: cfg.Execution.Real.Binance.TradeAPI.APISecretEnv,
		})
		grant := buildExecutionGrant(cfg)
		credentialRequirement := buildTradeCredentialRequirement(grant)
		credentialBroker := executioncred.NewBroker(executioncred.BrokerConfig{
			Boundary:   grant.Boundary,
			AdapterID:  grant.AdapterID,
			Mode:       grant.Mode,
			ResolverID: executioncred.ResolverIDTradeBrokerV1,
			ProviderID: executioncred.ProviderIDEnvStaticV1,
			SourceType: executioncred.SourceTypeEnv,
			SourceRef:  "execution.real.binance.trade_api",
		}, credentialProvider)
		client, err := executionadapterbinance.NewTradeAPIClient(executionadapterbinance.TradeAPIClientConfig{
			BaseURL:               cfg.Execution.Real.Binance.TradeAPI.BaseURL,
			RecvWindowMs:          cfg.Execution.Real.Binance.TradeAPI.RecvWindowMs,
			RequestTimeout:        cfg.Execution.Real.Binance.TradeAPI.RequestTimeoutDuration(),
			CredentialRequirement: credentialRequirement,
		}, credentialBroker)
		if err != nil {
			return nil, fmt.Errorf("executor: build binance trade api client: %w", err)
		}
		execCfg := executionadapterbinance.DefaultSafeIntentExecutorConfig()
		execCfg.EndpointMode = cfg.Execution.Real.Binance.TradeAPI.EndpointMode
		execCfg.ReconcileEnabled = cfg.Execution.Real.Binance.TradeAPI.ReconcileEnabled
		execCfg.ReconcilePollEvery = cfg.Execution.Real.Binance.TradeAPI.ReconcilePollIntervalDuration()
		execCfg.ReconcileMaxPolls = cfg.Execution.Real.Binance.TradeAPI.ReconcileMaxPolls
		realExecutor := executionadapterbinance.NewSafeIntentExecutor(execCfg, client)
		exec := executionapp.NewGovernedExecutor(executionapp.GovernedExecutorConfig{
			Governance: executionapp.NewStaticExecutionGovernance(executionapp.StaticExecutionGovernanceConfig{
				Authorizer: executionapp.StaticCapabilityAuthorizer{Grant: &grant},
				Selector: executionapp.NewStaticAdapterSelector(executionapp.AdapterRoute{
					Boundary:              grant.Boundary,
					AdapterID:             grant.AdapterID,
					Mode:                  grant.Mode,
					CredentialRequirement: credentialRequirement,
				}),
				CredentialResolver: credentialBroker,
				BoundaryInfo: executionports.BoundaryInfo{
					Boundary: grant.Boundary,
					Adapter:  grant.AdapterID,
					Mode:     grant.Mode,
				},
			}),
			Adapters: map[string]executionports.IntentExecutor{
				grant.AdapterID: realExecutor,
			},
			ControlPlane: controlPlane,
		})
		logger.Info("executor: using real adapter in safe mode",
			"mode", cfg.Execution.Mode,
			"adapter", cfg.Execution.Adapter,
			"governance_boundary", grant.Boundary,
			"allowed_venues", cfg.Execution.AllowedVenues,
			"allowed_symbols", cfg.Execution.AllowedSymbols,
			"allowed_accounts", cfg.Execution.AllowedAccounts,
			"max_abs_quantity", cfg.Execution.MaxAbsQuantity,
			"max_notional_usd", cfg.Execution.MaxNotionalUSD,
			"max_slippage_bps", cfg.Execution.MaxSlippageBps,
			"endpoint_mode", cfg.Execution.Real.Binance.TradeAPI.EndpointMode,
			"governance_grant", grant.GrantID,
			"credential_resolver", executioncred.ResolverIDTradeBrokerV1,
			"credential_provider", executioncred.ProviderIDEnvStaticV1,
			"credential_source_ref", "execution.real.binance.trade_api",
			"reconcile_enabled", cfg.Execution.Real.Binance.TradeAPI.ReconcileEnabled,
			"reconcile_poll_interval", cfg.Execution.Real.Binance.TradeAPI.ReconcilePollInterval,
			"reconcile_max_polls", cfg.Execution.Real.Binance.TradeAPI.ReconcileMaxPolls,
		)
		return exec, nil
	default:
		return nil, fmt.Errorf("executor: unsupported execution.mode=%q", cfg.Execution.Mode)
	}
}

func buildExecutionGrant(cfg config.AppConfig) executiongovernance.ExecutionGrant {
	mode := modeOrDefault(strings.ToLower(strings.TrimSpace(cfg.Execution.Mode)), "bootstrap_simulated")
	adapterID := strings.ToLower(strings.TrimSpace(cfg.Execution.Adapter))
	if mode == "bootstrap_simulated" && adapterID == "" {
		adapterID = "bootstrap.simulated"
	}
	grant := executiongovernance.ExecutionGrant{
		GrantID:   "execution." + mode + ".static_grant",
		Boundary:  "execution.adapter",
		AdapterID: adapterID,
		Mode:      mode,
		SafeMode:  cfg.Execution.SafeMode,
		TradeOnly: cfg.Execution.TradeOnly,
		Limits: executiongovernance.ExecutionLimits{
			MaxIntentTTLms: cfg.Execution.MaxIntentTTLms,
			MaxAbsQuantity: cfg.Execution.MaxAbsQuantity,
			MaxNotionalUSD: cfg.Execution.MaxNotionalUSD,
			MaxSlippageBps: cfg.Execution.MaxSlippageBps,
		},
		Provenance: executiongovernance.GrantProvenance{
			Source:   "config.execution",
			PolicyID: "stage9a.static_execution_governance",
		},
	}
	switch mode {
	case "bootstrap_simulated":
		grant.Scope = executiongovernance.ExecutionScope{
			AllowAnyVenue:   true,
			AllowAnySymbol:  true,
			AllowAnyAccount: true,
		}
	default:
		grant.Scope = executiongovernance.ExecutionScope{
			AllowedVenues:   setFromStrings(cfg.Execution.AllowedVenues, strings.ToLower),
			AllowedSymbols:  setFromStrings(cfg.Execution.AllowedSymbols, strings.ToUpper),
			AllowedAccounts: setFromStrings(cfg.Execution.AllowedAccounts, strings.TrimSpace),
		}
	}
	return grant
}

func setFromStrings(values []string, normalize func(string) string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if normalize != nil {
			v = normalize(v)
		}
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

func modeOrDefault(mode, fallback string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return fallback
	}
	return mode
}

// selectBootstrapAdapter returns the IntentExecutor for bootstrap_simulated mode.
// When execution.adapter is "simulation.deterministic", uses the SimulationEngine
// which provides realistic order lifecycle (partial fills, latency, cancellation).
// Otherwise falls back to the original BootstrapExecutor (instant fill).
func selectBootstrapAdapter(cfg config.AppConfig, grant executiongovernance.ExecutionGrant) executionports.IntentExecutor {
	adapterID := strings.ToLower(strings.TrimSpace(cfg.Execution.Adapter))
	if adapterID == "simulation.deterministic" {
		return executionapp.NewSimulationEngine(executionapp.SimulationConfig{
			ExecutionMode:  "bootstrap_simulated",
			MaxIntentTTLms: cfg.Execution.MaxIntentTTLms,
			MaxAbsQuantity: cfg.Execution.MaxAbsQuantity,
			MaxNotionalUSD: cfg.Execution.MaxNotionalUSD,
			MaxSlippageBps: cfg.Execution.MaxSlippageBps,
		})
	}
	return executionapp.NewBootstrapExecutor(executionapp.BootstrapConfig{
		MaxIntentTTLms: cfg.Execution.MaxIntentTTLms,
		MaxAbsQuantity: cfg.Execution.MaxAbsQuantity,
		MaxNotionalUSD: cfg.Execution.MaxNotionalUSD,
		MaxSlippageBps: cfg.Execution.MaxSlippageBps,
	})
}

func buildTradeCredentialRequirement(grant executiongovernance.ExecutionGrant) executiongovernance.CredentialRequirement {
	return executiongovernance.CredentialRequirement{
		Required:            true,
		Boundary:            strings.TrimSpace(grant.Boundary),
		AdapterID:           strings.TrimSpace(grant.AdapterID),
		Mode:                strings.TrimSpace(grant.Mode),
		Scope:               executioncred.ScopeTradeOnly,
		TradeOnly:           true,
		AcceptedResolverIDs: []string{executioncred.ResolverIDTradeBrokerV1},
		AcceptedProviderIDs: []string{executioncred.ProviderIDEnvStaticV1},
	}
}
