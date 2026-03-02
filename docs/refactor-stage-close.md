# Refactor Stage Close Checklist

Status: in-progress
Updated: 2026-03-02

## Mandatory Checklist

- [ ] No URL params for operational config
- [ ] No legacy connection bar / old Connection Settings wiring
- [ ] No widget parsing raw messages
- [ ] No widget IO calls
- [ ] Ring buffers only on continuous data path
- [ ] ParseArena and FrameArena ownership rules enforced
- [ ] Protocol gate (`HELLO/proto_ver/capabilities`) active
- [ ] Parser fails loudly on protocol mismatch (no silent fallback)
- [ ] Orderbook snapshot-before-delta invariant enforced
- [ ] Playwright E2E on `:8090` passed with screenshots/log evidence
- [ ] 15 min soak evidence captured (native RSS budget)
- [ ] wasm heap behavior documented (stable or bounded-growth mitigation)
