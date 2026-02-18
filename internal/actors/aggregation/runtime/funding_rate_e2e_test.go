package aggruntime_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/exchange/binance"
	"github.com/market-raccoon/internal/adapters/exchange/bybit"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

type fundingRateSequencer struct {
	n int64
}

func (s *fundingRateSequencer) Next(_, _ string) (int64, *problem.Problem) {
	s.n++
	return s.n, nil
}

func TestFundingRate_EndToEnd_BinanceMarkPrice(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	recvAt := time.UnixMilli(1700000005000)
	raw := []byte(`{"stream":"btcusdt@markPrice","data":{"e":"markPriceUpdate","E":1700000000000,"s":"BTCUSDT","p":"42000.50","i":"42001.00","r":"0.00010000"}}`)

	req, skip, p := binance.ParseMessage(raw, recvAt)
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}

	fakeClock := clock.NewFakeClock(recvAt)
	memBus := bus.NewInMemoryBus(8)
	sub := memBus.Subscribe()
	ingest := mdapp.NewIngestMarketData(fakeClock, &fundingRateSequencer{}, memBus)
	res := ingest.Execute(context.Background(), req)
	if res.IsFail() {
		t.Fatalf("ingest.Execute: %v", res.Problem())
	}

	select {
	case env := <-sub:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			t.Fatalf("DecodePayload: %v", p)
		}
		mark, ok := decoded.(domain.MarkPriceTickV1)
		if !ok {
			t.Fatalf("decoded type=%T", decoded)
		}
		if mark.FundingRate != 0.0001 {
			t.Fatalf("funding rate=%f want=0.0001", mark.FundingRate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for envelope")
	}
}

func TestFundingRate_EndToEnd_BybitTicker(t *testing.T) {
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry: %v", p)
	}

	recvAt := time.UnixMilli(1700000005000)
	raw := []byte(`{"topic":"tickers.BTCUSDT","type":"snapshot","ts":1700000000000,"data":{"symbol":"BTCUSDT","markPrice":"42000.50","indexPrice":"42001.00","fundingRate":"0.00010000"}}`)

	req, skip, p := bybit.ParseMessage(raw, recvAt)
	if p != nil || skip {
		t.Fatalf("ParseMessage failed: skip=%v problem=%v", skip, p)
	}

	fakeClock := clock.NewFakeClock(recvAt)
	memBus := bus.NewInMemoryBus(8)
	sub := memBus.Subscribe()
	ingest := mdapp.NewIngestMarketDataWithConfig(fakeClock, &fundingRateSequencer{}, memBus, mdapp.IngestConfig{
		PublishContentType: envelope.ContentTypeJSON,
	})
	res := ingest.Execute(context.Background(), req)
	if res.IsFail() {
		t.Fatalf("ingest.Execute: %v", res.Problem())
	}

	select {
	case env := <-sub:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			t.Fatalf("DecodePayload: %v", p)
		}
		mark, ok := decoded.(domain.MarkPriceTickV1)
		if !ok {
			t.Fatalf("decoded type=%T", decoded)
		}
		if mark.FundingRate != 0.0001 {
			t.Fatalf("funding rate=%f want=0.0001", mark.FundingRate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for envelope")
	}
}
