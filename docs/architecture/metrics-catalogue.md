# Metrics Catalogue

**Status:** Active
**Last updated:** 2026-06-25
**Relates to:** `internal/shared/metrics/metrics.go`, `deploy/observability/prometheus/`, `docs/architecture/README.md`

---

## Purpose

Unified registry of Prometheus metrics exposed by each Stream Analytics service binary.
All metrics are registered in `internal/shared/metrics/metrics.go` and exposed on
`:8080/metrics` (server), `:8081/metrics` (consumer), `:8082/metrics` (processor),
`:8083/metrics` (store), `:8089/metrics` (validator).

---

## Consumer (`cmd/consumer`)

### Ingestion Pipeline

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ingest_messages_total` | Counter | `venue`, `event_type`, `status` | Total messages processed; status: `ok`, `failed`, `duplicate`, `out_of_order`, `validation_failed` |
| `ingest_latency_seconds` | Histogram | `venue`, `event_type` | End-to-end ingest pipeline latency |
| `ingest_streams_active` | Gauge | — | Active ingest streams in memory |
| `canonicalization_errors_total` | Counter | `venue`, `reason` | CMM canonicalization failures |
| `canonical_events_total` | Counter | `channel`, `venue` | Canonical envelopes produced per channel |
| `canonical_state_entries` | Gauge | — | Per-stream dedup state entries |
| `canonical_state_evicted_total` | Counter | `reason` | Per-stream dedup state evictions |
| `ingest_quarantine_total` | Counter | `reason` | Poison envelopes routed to quarantine |
| `ingest_drop_total` | Counter | `reason` | Explicitly dropped ingest envelopes |
| `ingest_nak_total` | Counter | `reason` | NAKed JetStream messages |
| `ingest_term_total` | Counter | `reason` | TERMed JetStream messages |

### WebSocket Exchange Connections

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ws_connections_active` | Gauge | `venue` | Active exchange WebSocket connections |
| `ws_reconnects_total` | Counter | `venue`, `status` | Exchange WS reconnect attempts |
| `ws_messages_received_total` | Counter | `venue`, `event_type` | Raw WS messages received from exchanges |
| `ws_errors_total` | Counter | `venue`, `status` | Exchange WS errors |

### Backpressure

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `backpressure_queue_depth` | Gauge | `venue` | Current ingest backpressure queue depth |
| `backpressure_drops_total` | Counter | `policy` | Dropped messages under backpressure |

### NATS / Kafka Bus (Consumer side)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `bus_published_total` | Counter | `event_type`, `venue` | Messages published to NATS JetStream |
| `bus_publish_errors_total` | Counter | `kind` | NATS/Kafka publish errors |
| `bus_publish_latency_seconds` | Histogram | `bus_type` | Publish call latency |
| `bus_dropped_total` | Counter | `subscriber_id` | Dropped fan-out messages per subscriber |

---

## Processor (`cmd/processor`)

### Aggregation — OrderBook

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aggregation_books_active` | Gauge | — | Active order books in memory |
| `mr_orderbook_levels_total` | Gauge | `venue`, `instrument_bucket`, `side` | Current OB levels per side |
| `mr_orderbook_spread_bps` | Gauge | `venue`, `instrument_bucket` | Current bid-ask spread in bps |
| `mr_orderbook_update_duration_seconds` | Histogram | `venue` | OB apply operation latency |
| `mr_orderbook_gap_total` | Counter | `venue`, `instrument_bucket` | Sequence gaps between accepted updates |
| `mr_orderbook_stale_total` | Counter | `venue`, `instrument_bucket` | Out-of-order deltas |
| `mr_orderbook_bad_level_total` | Counter | `venue`, `instrument_bucket`, `reason` | Rejected OB levels |
| `mr_orderbook_prune_total` | Counter | `venue`, `instrument_bucket` | Pruned levels (cap enforcement) |
| `mr_orderbook_crossed_total` | Counter | `venue`, `instrument_bucket` | Crossed-book detections |

### Aggregation — Trade / Tape / Candles

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mr_trade_ingest_total` | Counter | `venue` | Trades successfully ingested |
| `mr_trade_latency_seconds` | Histogram | `venue` | Exchange → MR ingest latency |
| `mr_trade_duplicate_total` | Counter | `venue` | Duplicate trades dropped |
| `mr_trade_out_of_order_total` | Counter | `venue`, `instrument_bucket` | Out-of-order trades |
| `mr_tape_quality_flags_total` | Counter | `venue`, `instrument_bucket`, `timeframe_bucket`, `flag` | Tape window quality flags |
| `mr_stats_quality_flags_total` | Counter | `venue`, `instrument_bucket`, `timeframe_bucket`, `flag` | Stats window quality flags |
| `mr_window_open_total` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket` | Open event-time windows |
| `mr_window_late_arrival_total` | Counter | `venue`, `instrument_bucket`, `timeframe_bucket` | Late events dropped by watermark |
| `mr_window_force_close_total` | Counter | `venue`, `instrument_bucket`, `timeframe_bucket` | Windows force-closed by hard cap |
| `processor_processed_total` | Counter | `event_type`, `status` | Envelopes processed by aggregation actor |
| `processor_commit_total` | Counter | `status` | Snapshot commit operations |
| `processor_commit_latency_seconds` | Histogram | — | Hot+cold dual-write commit latency |

### Aggregation — Cross-Venue

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mr_xvenue_spread_bps` | Gauge | `instrument_bucket` | Cross-venue best-spread in bps |
| `mr_xvenue_divergence_bps` | Gauge | `instrument_bucket` | Cross-venue spread divergence |
| `mr_xvenue_merge_duration_seconds` | Histogram | `instrument_bucket` | Cross-venue merge operation latency |
| `mr_xvenue_venues_active` | Gauge | `instrument_bucket` | Active (non-stale) venues in merge |
| `insights_snapshots_total` | Counter | `venue_count_bucket` | Cross-venue insight snapshots emitted |
| `insights_state_instruments_active` | Gauge | — | Active instrument states in join cache |

### Insights — VPVR / Heatmap

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `heatmap_build_latency_seconds` | Histogram | `venue`, `instrument_bucket`, `timeframe_bucket` | Heatmap build latency |
| `heatmap_cells_total` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket` | Emitted heatmap cell count |
| `heatmap_payload_bytes` | Histogram | `venue`, `instrument_bucket`, `timeframe_bucket` | Heatmap payload size |
| `heatmap_drop_total` | Counter | `reason` | Heatmap drops/degradations |
| `vpvr_builder_bucket_count` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket` | VPVR bucket count per window |
| `vpvr_overload_level` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket` | VPVR overload level |
| `vpvr_drop_total` | Counter | `reason` | VPVR emit-path drops |
| `policykit_overload_level` | Gauge | `stream`, `venue` | PolicyKit overload level |
| `policykit_drop_total` | Counter | `stream`, `venue` | PolicyKit drops |

### Evidence (LEL)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `evidence_emitted_total` | Counter | `type`, `severity`, `venue` | Canonical evidence events emitted |
| `evidence_dropped_total` | Counter | `reason` | Evidence events dropped |
| `evidence_state_entries` | Gauge | — | Per-stream evidence state entries |
| `evidence_eval_latency_seconds` | Histogram | — | Evidence evaluation span |
| `lel_evidence_emitted_total` | Counter | `type`, `severity`, `venue` | LEL v1 evidence events emitted |
| `lel_evidence_dropped_total` | Counter | `reason` | LEL evidence events dropped |
| `lel_state_entries` | Gauge | — | LEL state-store active entries |
| `lel_eval_latency_seconds` | Histogram | — | LEL rule evaluation latency |
| `lel_input_processed_total` | Counter | `kind` | LEL input events processed |

### Regime Detection

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `mr_regime_current` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket`, `kind` | Current regime (one-hot, value=1) |
| `mr_regime_strength` | Gauge | `venue`, `instrument_bucket`, `timeframe_bucket` | Regime strength [0.0, 1.0] |
| `mr_regime_transition_total` | Counter | `venue`, `instrument_bucket`, `timeframe_bucket`, `from`, `to` | Regime transitions |

### Sharding (multi-replica processor)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `jetstream_shard_consumer_lag` | Gauge | `group_id` | Consumer lag (NumPending) per shard |
| `jetstream_shard_events_total` | Counter | `group_id` | Events processed per shard |
| `jetstream_shard_redelivered_total` | Counter | `group_id` | Redelivered messages per shard |
| `jetstream_shard_ack_latency_seconds` | Histogram | `group_id` | Ack latency per shard |
| `jetstream_shard_skip_total` | Counter | `group_id` | Messages skipped (different shard) |
| `shard_topology_complete` | Gauge | — | 1=all shard owners present |
| `shard_lease_age_seconds` | Gauge | — | Age since last lease heartbeat |
| `shard_owner_conflicts_total` | Counter | — | Dual-owner conflict detections |
| `shard_lease_lost_total` | Counter | — | Lease-lost events triggering shutdown |

---

## Server (`cmd/server`)

### WebSocket Delivery

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ws_clients_connected` | Gauge | — | Connected WS delivery clients |
| `ws_subscriptions_active` | Gauge | — | Active WS subscriptions |
| `ws_drops_total` | Counter | `reason` | Dropped outbound events |
| `ws_lag_seconds` | Gauge | `channel` | Delivery lag in seconds |
| `ws_send_latency_seconds` | Histogram | — | Frame write latency |
| `ws_publish_to_deliver_latency_seconds` | Histogram | `channel` | Publish → WS delivery latency |
| `ws_messages_out_total` | Counter | `channel` | Outbound messages by channel |
| `ws_bytes_out_total` | Counter | `channel` | Outbound bytes by channel |
| `ws_query_total` | Counter | `op`, `bc` | Read-path queries by operation |
| `ws_query_rejected_total` | Counter | `reason` | Rejected read-path queries |
| `ws_limit_rejections_total` | Counter | `type` | Rejections due to session limits |
| `ws_resync_total` | Counter | — | Resync requests handled |
| `ws_contract_violations_total` | Counter | `reason` | WS protocol contract violations |
| `ws_control_frames_total` | Counter | `type` | Control frames sent |

### Delivery Router

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `delivery_router_subscriptions_active` | Gauge | — | Active subject subscriptions |
| `delivery_router_events_routed_total` | Counter | — | Events routed to ≥1 session |
| `delivery_router_events_rejected_total` | Counter | `reason` | Events rejected by router |
| `delivery_router_sessions_active` | Gauge | — | Active router sessions |
| `delivery_router_coherence_violations_total` | Counter | `type`, `reason` | Stream coherence violations |
| `delivery_ws_snapshot_cache_entries` | Gauge | — | Snapshot cache entries |
| `delivery_ws_snapshot_cache_hits_total` | Counter | — | Snapshot cache hits |

### WS Compression / Batching

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ws_compress_applied_total` | Counter | — | Frames where compression applied |
| `ws_batch_frames_total` | Counter | — | Batched frames emitted |
| `ws_batch_events_total` | Counter | — | Events inside batched frames |

---

## Store (`cmd/store`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `store_consumed_total` | Counter | `status`, `reason` | Envelopes consumed by store pipeline |
| `store_commit_total` | Counter | `status` | ClickHouse commit operations |
| `store_commit_latency_seconds` | Histogram | — | ClickHouse commit latency |
| `store_flush_total` | Counter | `status` | Batch flush operations |
| `store_flush_latency_seconds` | Histogram | — | Batch flush latency |
| `store_batch_size` | Histogram | — | Rows per flushed batch |
| `store_quarantine_total` | Counter | `reason` | Quarantined envelopes |

---

## Validator (`cmd/validator`)

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dataplane_validation_total` | Counter | `status` | Validations by status (`ok`, `violation`, `error`) |
| `dataplane_validation_violations_total` | Counter | — | Field violations detected |
| `dataplane_messages_consumed_total` | Counter | — | JetStream messages consumed |

---

## Cross-Service (all binaries)

### Guardian Supervision

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `guardian_restarts_total` | Counter | `subsystem`, `status` | Subsystem restarts |
| `guardian_degraded_total` | Counter | `subsystem` | Degraded transitions |
| `guardian_subsystem_state` | Gauge | `subsystem` | Current state (0=stopped, 1=running, 2=degraded) |
| `guardian_rate_limited_total` | Counter | — | Restart attempts deferred by global limiter |

### JetStream Consumer Bus

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `bus_consumed_total` | Counter | `bus_type`, `status` | Consumed messages by status |
| `bus_redelivered_total` | Counter | `bus_type` | Redelivered messages |
| `bus_ack_latency_seconds` | Histogram | `bus_type` | Ack/Nak/Term operation latency |
| `bus_consumer_lag` | Gauge | `bus_type` | JetStream consumer lag (NumPending) |

### Process Runtime

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `process_goroutines` | Gauge | — | Current goroutine count |
| `process_heap_alloc_bytes` | Gauge | — | Current heap allocations |
| `process_gc_pause_seconds` | Histogram | — | GC pause durations |

### SLO Runtime

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `slo_breach_active` | Gauge | `name` | Whether SLO is currently in breach (0/1) |
| `slo_error_budget_remaining_ratio` | Gauge | `name` | Remaining error budget ratio [0.0–1.0] |
| `slo_burn_rate_fast` | Gauge | `name` | Fast burn rate |
| `slo_burn_rate_slow` | Gauge | `name` | Slow burn rate |

---

## Deprecated Metrics (remove by 2026-06-30)

| Metric | Replacement |
|--------|------------|
| `ws_send_latency_ms` | `ws_send_latency_seconds` |
| `ws_publish_to_deliver_latency_ms` | `ws_publish_to_deliver_latency_seconds` |
| `ws_lag_ms` | `ws_lag_seconds` |

---

## Code Anchor

All metrics are defined in `internal/shared/metrics/metrics.go`. They are registered once
via `init()` → `registerAll()` (sync.Once) and exposed on each binary's HTTP server via
the shared Prometheus registry.

Grafana dashboards: `deploy/observability/grafana/dashboards/` (5 pre-provisioned dashboards).
Alert rules: `deploy/observability/prometheus/*.yml`.
