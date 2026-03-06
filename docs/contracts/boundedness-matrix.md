# Boundedness Matrix

Source of truth for bounded caps/budgets enforced by IQ/CI drift validation.
Any effective cap/budget change must update this matrix in the same change.

Catalog: `s18-p1-v2`
Updated at: `2026-03-05`

## Matrix By Subsystem

### backend.aggregation

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `backend.aggregation.candle_max_windows` | `processor_candle_max_windows` | 50000 | windows |
| `backend.aggregation.candle_window_cap` | `processor_candle_window_cap` | 96 | windows |
| `backend.aggregation.stats_max_windows` | `processor_stats_max_windows` | 50000 | windows |
| `backend.aggregation.stats_window_cap` | `processor_stats_window_cap` | 96 | windows |

### backend.delivery

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `backend.delivery.router_stream_state_entries_max` | `router_stream_state_map` | 50000 | entries |
| `backend.delivery.session_max_frame_bytes` | `ws_session_max_frame` | 65536 | bytes |
| `backend.delivery.session_outbound_queue_size` | `ws_session_outbound_queue` | 512 | entries |

### backend.evidence

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `backend.evidence.buffer_cap_per_kind` | `evidence_buffer_per_kind` | 1000 | entries |
| `backend.evidence.regime_history_cap` | `regime_history_ring` | 20 | entries |
| `backend.evidence.regime_max_streams` | `regime_max_streams` | 1024 | entries |

### backend.signal

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `backend.signal.global_rate_limit_per_min` | `signal_global_rate_limit` | 100 | events_per_min |
| `backend.signal.max_subs_per_session` | `signal_subscriptions_per_session` | 20 | entries |
| `backend.signal.rate_limit_per_min` | `signal_rate_limit_per_session` | 10 | events_per_min |
| `backend.signal.window_cap` | `signal_window_cap` | 50 | entries |

### client.native.marketdata

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `client.native.candle_ring_cap` | `candle_ring` | 8 | entries |
| `client.native.signal_ring_cap` | `signal_ring` | 64 | entries |
| `client.native.trade_ring_cap` | `trade_ring` | 1024 | entries |

### client.web.marketdata

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `client.web.candle_ring_cap` | `candle_ring` | 32 | entries |
| `client.web.signal_ring_cap` | `signal_ring` | 64 | entries |
| `client.web.trade_ring_cap` | `trade_ring` | 1024 | entries |

### client.widgets

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `client.widgets.dom_max_entries` | `dom_store` | 100 | entries |
| `client.widgets.evidence_max_entries` | `evidence_ring` | 96 | entries |
| `client.widgets.signal_max_entries` | `signal_store` | 400 | entries |
| `client.widgets.stats_max_entries` | `stats_store` | 64 | entries |
| `client.widgets.tape_max_entries` | `trades_tape_store` | 256 | entries |

### iq.invariants

| id | structure | cap | unit |
| --- | --- | ---: | --- |
| `iq.layer_stream_state_max_budget` | `layer_stream_state_budget` | 2048 | entries |
| `iq.router_stream_state_max_budget` | `router_stream_state_budget` | 2048 | entries |
| `iq.wire_bytes_p95_budget_default` | `wire_bytes_p95_budget` | 65536 | bytes |
| `iq.wire_bytes_p99_budget_default` | `wire_bytes_p99_budget` | 131072 | bytes |

## Machine-Readable Matrix

<!-- boundedness-matrix:data:start -->
```json
{
  "catalog_version": "s18-p1-v2",
  "updated_at": "2026-03-05",
  "entries": [
    {
      "id": "backend.aggregation.candle_max_windows",
      "subsystem": "backend.aggregation",
      "structure": "processor_candle_max_windows",
      "cap": 50000,
      "unit": "windows",
      "default": "applyDefaults processor.candle.max_candles",
      "override": "processor.candle.max_candles",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Processor.Candle.MaxCandles = 50_000"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "processor.candle.max_candles must be > 0"
        }
      ]
    },
    {
      "id": "backend.aggregation.candle_window_cap",
      "subsystem": "backend.aggregation",
      "structure": "processor_candle_window_cap",
      "cap": 96,
      "unit": "windows",
      "default": "applyDefaults processor.candle.window_cap",
      "override": "processor.candle.window_cap",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Processor.Candle.WindowCap = 96"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "processor.candle.window_cap must be > 0"
        }
      ]
    },
    {
      "id": "backend.aggregation.stats_max_windows",
      "subsystem": "backend.aggregation",
      "structure": "processor_stats_max_windows",
      "cap": 50000,
      "unit": "windows",
      "default": "applyDefaults processor.stats.max_windows",
      "override": "processor.stats.max_windows",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Processor.Stats.MaxWindows = 50_000"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "processor.stats.max_windows must be > 0"
        }
      ]
    },
    {
      "id": "backend.aggregation.stats_window_cap",
      "subsystem": "backend.aggregation",
      "structure": "processor_stats_window_cap",
      "cap": 96,
      "unit": "windows",
      "default": "applyDefaults processor.stats.window_cap",
      "override": "processor.stats.window_cap",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Processor.Stats.WindowCap = 96"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "processor.stats.window_cap must be > 0"
        }
      ]
    },
    {
      "id": "backend.delivery.router_stream_state_entries_max",
      "subsystem": "backend.delivery",
      "structure": "router_stream_state_map",
      "cap": 50000,
      "unit": "entries",
      "default": "50000 in router runtime",
      "override": "router config MaxStreamStateEntries (internal wiring)",
      "metric": "router_stream_state_entries",
      "anchors": [
        {
          "file": "internal/actors/delivery/runtime/router.go",
          "line": 100,
          "snippet": "defaultMaxStreamStateEntries         = 50000"
        },
        {
          "file": "internal/shared/metrics/metrics.go",
          "line": 1598,
          "snippet": "Name: \"router_stream_state_entries\""
        }
      ]
    },
    {
      "id": "backend.delivery.session_max_frame_bytes",
      "subsystem": "backend.delivery",
      "structure": "ws_session_max_frame",
      "cap": 65536,
      "unit": "bytes",
      "default": "64*1024 readLimitBytes fallback",
      "override": "delivery.max_frame_bytes (config), ws.tenant_limits.<tenant>.max_frame_bytes",
      "metric": "ws_effective_limits{type=max_frame_bytes}",
      "anchors": [
        {
          "file": "internal/actors/delivery/runtime/session.go",
          "line": 25,
          "snippet": "const readLimitBytes = 64 * 1024"
        },
        {
          "file": "internal/actors/delivery/runtime/effective_limits.go",
          "line": 37,
          "snippet": "el.MaxFrameBytes = readLimitBytes"
        }
      ]
    },
    {
      "id": "backend.delivery.session_outbound_queue_size",
      "subsystem": "backend.delivery",
      "structure": "ws_session_outbound_queue",
      "cap": 512,
      "unit": "entries",
      "default": "512 via applyDefaults()",
      "override": "delivery.session_outbound_queue_size (config), ws.tenant_limits.<tenant>.outbound_queue_size",
      "metric": "ws_queue_capacity",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "line": 1377,
          "snippet": "c.Delivery.SessionOutboundQueueSize = 512"
        },
        {
          "file": "internal/shared/metrics/metrics.go",
          "line": 1628,
          "snippet": "Name: \"ws_queue_capacity\""
        }
      ]
    },
    {
      "id": "backend.evidence.buffer_cap_per_kind",
      "subsystem": "backend.evidence",
      "structure": "evidence_buffer_per_kind",
      "cap": 1000,
      "unit": "entries",
      "default": "applyDefaults evidence.buffer_cap_per_kind",
      "override": "evidence.buffer_cap_per_kind",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Evidence.BufferCapPerKind = 1000"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "evidence.buffer_cap_per_kind must be > 0"
        }
      ]
    },
    {
      "id": "backend.evidence.regime_history_cap",
      "subsystem": "backend.evidence",
      "structure": "regime_history_ring",
      "cap": 20,
      "unit": "entries",
      "default": "applyDefaults evidence.regime_history_cap",
      "override": "evidence.regime_history_cap",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Evidence.RegimeHistoryCap = 20"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "evidence.regime_history_cap must be > 0"
        }
      ]
    },
    {
      "id": "backend.evidence.regime_max_streams",
      "subsystem": "backend.evidence",
      "structure": "regime_max_streams",
      "cap": 1024,
      "unit": "entries",
      "default": "applyDefaults evidence.regime_max_streams",
      "override": "evidence.regime_max_streams",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Evidence.RegimeMaxStreams = 1024"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "evidence.regime_max_streams must be > 0"
        }
      ]
    },
    {
      "id": "backend.signal.global_rate_limit_per_min",
      "subsystem": "backend.signal",
      "structure": "signal_global_rate_limit",
      "cap": 100,
      "unit": "events_per_min",
      "default": "applyDefaults signals.global_rate_limit_per_min",
      "override": "signals.global_rate_limit_per_min",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Signals.GlobalRateLimitPerMin = 100"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "signals.global_rate_limit_per_min must be > 0"
        }
      ]
    },
    {
      "id": "backend.signal.max_subs_per_session",
      "subsystem": "backend.signal",
      "structure": "signal_subscriptions_per_session",
      "cap": 20,
      "unit": "entries",
      "default": "applyDefaults signals.max_subs_per_session",
      "override": "signals.max_subs_per_session",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Signals.MaxSubsPerSession = 20"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "signals.max_subs_per_session must be > 0"
        }
      ]
    },
    {
      "id": "backend.signal.rate_limit_per_min",
      "subsystem": "backend.signal",
      "structure": "signal_rate_limit_per_session",
      "cap": 10,
      "unit": "events_per_min",
      "default": "applyDefaults signals.rate_limit_per_min",
      "override": "signals.rate_limit_per_min",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Signals.RateLimitPerMin = 10"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "signals.rate_limit_per_min must be > 0"
        }
      ]
    },
    {
      "id": "backend.signal.window_cap",
      "subsystem": "backend.signal",
      "structure": "signal_window_cap",
      "cap": 50,
      "unit": "entries",
      "default": "applyDefaults signals.window_cap",
      "override": "signals.window_cap",
      "metric": "config_runtime_limits",
      "anchors": [
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "c.Signals.WindowCap = 50"
        },
        {
          "file": "internal/shared/config/loader.go",
          "snippet": "signals.window_cap must be > 0"
        }
      ]
    },
    {
      "id": "client.native.candle_ring_cap",
      "subsystem": "client.native.marketdata",
      "structure": "candle_ring",
      "cap": 8,
      "unit": "entries",
      "default": "native candle ring hard cap",
      "override": "none",
      "metric": "probe_md_candle_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/native/marketdata_native.odin",
          "line": 30,
          "snippet": "CANDLE_RING_CAP :: 8"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 621,
          "snippet": "probe_md_candle_backlog_cap"
        }
      ]
    },
    {
      "id": "client.native.signal_ring_cap",
      "subsystem": "client.native.marketdata",
      "structure": "signal_ring",
      "cap": 64,
      "unit": "entries",
      "default": "native signal ring hard cap",
      "override": "none",
      "metric": "probe_md_signal_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/native/marketdata_native.odin",
          "line": 31,
          "snippet": "SIGNAL_RING_CAP :: 64"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 635,
          "snippet": "probe_md_signal_backlog_cap"
        }
      ]
    },
    {
      "id": "client.native.trade_ring_cap",
      "subsystem": "client.native.marketdata",
      "structure": "trade_ring",
      "cap": 1024,
      "unit": "entries",
      "default": "native trade ring hard cap",
      "override": "none",
      "metric": "probe_md_trade_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/native/marketdata_native.odin",
          "line": 29,
          "snippet": "TRADE_RING_CAP  :: 1024"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 607,
          "snippet": "probe_md_trade_backlog_cap"
        }
      ]
    },
    {
      "id": "client.web.candle_ring_cap",
      "subsystem": "client.web.marketdata",
      "structure": "candle_ring",
      "cap": 32,
      "unit": "entries",
      "default": "web candle ring hard cap",
      "override": "none",
      "metric": "probe_md_candle_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/web/marketdata_web.odin",
          "line": 42,
          "snippet": "WEB_CANDLE_RING_CAP  :: 32"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 621,
          "snippet": "probe_md_candle_backlog_cap"
        }
      ]
    },
    {
      "id": "client.web.signal_ring_cap",
      "subsystem": "client.web.marketdata",
      "structure": "signal_ring",
      "cap": 64,
      "unit": "entries",
      "default": "web signal ring hard cap",
      "override": "none",
      "metric": "probe_md_signal_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/web/marketdata_web.odin",
          "line": 43,
          "snippet": "WEB_SIGNAL_RING_CAP  :: 64"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 635,
          "snippet": "probe_md_signal_backlog_cap"
        }
      ]
    },
    {
      "id": "client.web.trade_ring_cap",
      "subsystem": "client.web.marketdata",
      "structure": "trade_ring",
      "cap": 1024,
      "unit": "entries",
      "default": "web trade ring hard cap",
      "override": "none",
      "metric": "probe_md_trade_backlog_cap",
      "anchors": [
        {
          "file": "client/src/platform/web/marketdata_web.odin",
          "line": 41,
          "snippet": "WEB_TRADE_RING_CAP   :: 1024"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 607,
          "snippet": "probe_md_trade_backlog_cap"
        }
      ]
    },
    {
      "id": "client.widgets.dom_max_entries",
      "subsystem": "client.widgets",
      "structure": "dom_store",
      "cap": 100,
      "unit": "entries",
      "default": "services.OB_DEPTH_CAP * 2",
      "override": "IQ_WIDGET_MAX_ENTRIES / IQ_WIDGET_MAX_ENTRIES_BY_WIDGET (budget only)",
      "metric": "probe_widget_dom_max_entries",
      "anchors": [
        {
          "file": "client/src/core/services/orderbook_store.odin",
          "line": 9,
          "snippet": "OB_DEPTH_CAP :: 50"
        },
        {
          "file": "client/src/core/layers/layer_strategies.odin",
          "line": 349,
          "snippet": "out.max_entries = services.OB_DEPTH_CAP * 2"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 852,
          "snippet": "probe_widget_dom_max_entries"
        }
      ]
    },
    {
      "id": "client.widgets.evidence_max_entries",
      "subsystem": "client.widgets",
      "structure": "evidence_ring",
      "cap": 96,
      "unit": "entries",
      "default": "EVIDENCE_RING_CAP",
      "override": "IQ_WIDGET_MAX_ENTRIES / IQ_WIDGET_MAX_ENTRIES_BY_WIDGET (budget only)",
      "metric": "probe_widget_evidence_max_entries",
      "anchors": [
        {
          "file": "client/src/core/layers/market_store.odin",
          "line": 7,
          "snippet": "EVIDENCE_RING_CAP  :: 96"
        },
        {
          "file": "client/src/core/layers/layer_strategies.odin",
          "line": 489,
          "snippet": "out.max_entries = EVIDENCE_RING_CAP"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 1020,
          "snippet": "probe_widget_evidence_max_entries"
        }
      ]
    },
    {
      "id": "client.widgets.signal_max_entries",
      "subsystem": "client.widgets",
      "structure": "signal_store",
      "cap": 400,
      "unit": "entries",
      "default": "services.SIGNAL_KIND_CAP * services.SIGNAL_PER_KIND_CAP",
      "override": "IQ_WIDGET_MAX_ENTRIES / IQ_WIDGET_MAX_ENTRIES_BY_WIDGET (budget only)",
      "metric": "probe_widget_signal_max_entries",
      "anchors": [
        {
          "file": "client/src/core/services/signal_store.odin",
          "line": 7,
          "snippet": "SIGNAL_KIND_CAP     :: 8"
        },
        {
          "file": "client/src/core/services/signal_store.odin",
          "line": 8,
          "snippet": "SIGNAL_PER_KIND_CAP :: 50"
        },
        {
          "file": "client/src/core/layers/layer_strategies.odin",
          "line": 559,
          "snippet": "out.max_entries = services.SIGNAL_KIND_CAP * services.SIGNAL_PER_KIND_CAP"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 1104,
          "snippet": "probe_widget_signal_max_entries"
        }
      ]
    },
    {
      "id": "client.widgets.stats_max_entries",
      "subsystem": "client.widgets",
      "structure": "stats_store",
      "cap": 64,
      "unit": "entries",
      "default": "services.STATS_CAP",
      "override": "IQ_WIDGET_MAX_ENTRIES / IQ_WIDGET_MAX_ENTRIES_BY_WIDGET (budget only)",
      "metric": "probe_widget_stats_max_entries",
      "anchors": [
        {
          "file": "client/src/core/services/stats_store.odin",
          "line": 17,
          "snippet": "STATS_CAP :: 64"
        },
        {
          "file": "client/src/core/layers/layer_strategies.odin",
          "line": 180,
          "snippet": "out.max_entries = services.STATS_CAP"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 229,
          "snippet": "probe_widget_stats_max_entries"
        }
      ]
    },
    {
      "id": "client.widgets.tape_max_entries",
      "subsystem": "client.widgets",
      "structure": "trades_tape_store",
      "cap": 256,
      "unit": "entries",
      "default": "services.TRADES_CAP",
      "override": "IQ_WIDGET_MAX_ENTRIES / IQ_WIDGET_MAX_ENTRIES_BY_WIDGET (budget only)",
      "metric": "probe_widget_tape_max_entries",
      "anchors": [
        {
          "file": "client/src/core/services/trades_store.odin",
          "line": 18,
          "snippet": "TRADES_CAP :: 256"
        },
        {
          "file": "client/src/core/layers/layer_strategies.odin",
          "line": 259,
          "snippet": "out.max_entries = services.TRADES_CAP"
        },
        {
          "file": "client/src/platform/web/main.odin",
          "line": 936,
          "snippet": "probe_widget_tape_max_entries"
        }
      ]
    },
    {
      "id": "iq.layer_stream_state_max_budget",
      "subsystem": "iq.invariants",
      "structure": "layer_stream_state_budget",
      "cap": 2048,
      "unit": "entries",
      "default": "inherits router stream-state budget",
      "override": "IQ_LAYER_STREAM_STATE_MAX",
      "metric": "layer_stream_bounded",
      "anchors": [
        {
          "file": "scripts/iq/analyze_iq_run.mjs",
          "line": 1067,
          "snippet": "IQ_LAYER_STREAM_STATE_MAX\", String(routerStateMaxEntries))"
        }
      ]
    },
    {
      "id": "iq.router_stream_state_max_budget",
      "subsystem": "iq.invariants",
      "structure": "router_stream_state_budget",
      "cap": 2048,
      "unit": "entries",
      "default": "IQ_ROUTER_STREAM_STATE_MAX default in analyze script",
      "override": "IQ_ROUTER_STREAM_STATE_MAX",
      "metric": "bounded_state_eviction",
      "anchors": [
        {
          "file": "scripts/iq/analyze_iq_run.mjs",
          "line": 1066,
          "snippet": "IQ_ROUTER_STREAM_STATE_MAX\", \"2048\")"
        }
      ]
    },
    {
      "id": "iq.wire_bytes_p95_budget_default",
      "subsystem": "iq.invariants",
      "structure": "wire_bytes_p95_budget",
      "cap": 65536,
      "unit": "bytes",
      "default": "CRITICAL_PROFILE_DEFAULTS",
      "override": "IQ_WIRE_BYTES_P95_BUDGET",
      "metric": "ws_wire_bytes p95",
      "anchors": [
        {
          "file": "scripts/iq/profile_loader.mjs",
          "line": 24,
          "snippet": "IQ_WIRE_BYTES_P95_BUDGET: \"65536\""
        },
        {
          "file": "scripts/iq/profiles/ci-strict.env",
          "line": 18,
          "snippet": "IQ_WIRE_BYTES_P95_BUDGET=65536"
        }
      ]
    },
    {
      "id": "iq.wire_bytes_p99_budget_default",
      "subsystem": "iq.invariants",
      "structure": "wire_bytes_p99_budget",
      "cap": 131072,
      "unit": "bytes",
      "default": "CRITICAL_PROFILE_DEFAULTS",
      "override": "IQ_WIRE_BYTES_P99_BUDGET",
      "metric": "ws_wire_bytes p99",
      "anchors": [
        {
          "file": "scripts/iq/profile_loader.mjs",
          "line": 25,
          "snippet": "IQ_WIRE_BYTES_P99_BUDGET: \"131072\""
        },
        {
          "file": "scripts/iq/profiles/ci-strict.env",
          "line": 19,
          "snippet": "IQ_WIRE_BYTES_P99_BUDGET=131072"
        }
      ]
    }
  ]
}
```
<!-- boundedness-matrix:data:end -->
