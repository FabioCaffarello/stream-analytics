package contracts

import (
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/problem"
	executionv1 "github.com/market-raccoon/internal/shared/proto/gen/execution/v1"
	portfoliov1 "github.com/market-raccoon/internal/shared/proto/gen/portfolio/v1"
	strategyv1 "github.com/market-raccoon/internal/shared/proto/gen/strategy/v1"
)

func RegisterStrategyPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	return registerPayloadDual(
		reg,
		strategydomain.IntentEventType,
		codec.JSONCodec[strategydomain.StrategyIntentV1]{},
		domainProtoPayloadCodec[strategydomain.StrategyIntentV1, *strategyv1.StrategyIntentV1]{
			newProto: func() *strategyv1.StrategyIntentV1 { return &strategyv1.StrategyIntentV1{} },
			toProto:  DomainToProtoStrategyIntentV1,
			toDomain: ProtoToDomainStrategyIntentV1,
		},
	)
}

func RegisterExecutionPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	return registerPayloadDual(
		reg,
		executiondomain.EventType,
		codec.JSONCodec[executiondomain.ExecutionEventV1]{},
		domainProtoPayloadCodec[executiondomain.ExecutionEventV1, *executionv1.ExecutionEventV1]{
			newProto: func() *executionv1.ExecutionEventV1 { return &executionv1.ExecutionEventV1{} },
			toProto:  DomainToProtoExecutionEventV1,
			toDomain: ProtoToDomainExecutionEventV1,
		},
	)
}

func RegisterPortfolioPayloadV1(reg *codec.Registry) *problem.Problem {
	if reg == nil {
		return problem.New(problem.ValidationFailed, "codec registry must not be nil")
	}
	return registerPayloadDual(
		reg,
		portfoliodomain.StateEventType,
		codec.JSONCodec[portfoliodomain.PortfolioStateV1]{},
		domainProtoPayloadCodec[portfoliodomain.PortfolioStateV1, *portfoliov1.PortfolioStateV1]{
			newProto: func() *portfoliov1.PortfolioStateV1 { return &portfoliov1.PortfolioStateV1{} },
			toProto:  DomainToProtoPortfolioStateV1,
			toDomain: ProtoToDomainPortfolioStateV1,
		},
	)
}
