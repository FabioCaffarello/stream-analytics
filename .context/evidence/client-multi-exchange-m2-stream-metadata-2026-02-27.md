# Client Multi-Exchange M2 Stream Metadata (2026-02-27)

## Objective
Consolidar metadata canônica de stream no core (`App_State`/`Stream_View_Slot`) para reduzir lookup ad-hoc no port e aumentar robustez em troca/reconnect de stream.

## Changes Applied
- `client/src/core/app/app.odin`
  - `Stream_View_Slot` agora inclui:
    - `has_stream_info: bool`
    - `stream_info: ports.MD_Stream_Info`
  - adicionada funcao `refresh_stream_info_for_slot` para hidratar cache do slot por `subject_id`.
  - `persist_active_stream_subject` agora usa metadata cacheada do slot ativo (com refresh lazy), em vez de chamar `describe_stream` diretamente a cada persist.
  - `handle_tf_hotkeys` agora monta `getrange` usando metadata cacheada do stream ativo (com fallback de refresh lazy).
  - `drain_marketdata` passa a hidratar metadata do slot conforme os eventos chegam.
  - `draw_top_bar` passa a usar metadata cacheada do slot ativo para nome/timeframe/canal.

## Validation Commands
```bash
make -C client check-core
make -C client build-native
make -C client build-wasm
make smoke
make -C client run-native-compose NATIVE_FLAGS='--soak-seconds=20 --soak-log-ms=1000 --soak-multi'
```

## Results
- `check-core`: `PASS`
- `build-native`: `PASS`
- `build-wasm`: `PASS`
- `smoke`: `PASS`
- native quick run-compose soak (20s): `PASS` com ACKs multi-venue e sem drop/reconnect anomalo.

## Playwright MCP Check
- `http://127.0.0.1:8090/` carregado com conexao WS e ACKs de subscribe.
- restart de `server` durante sessao ativa:
  - erros de handshake transitorios esperados;
  - reconnect + resubscribe confirmados ao final.

## Notes
- Esta rodada fecha a consolidacao base de metadata por stream no core.
- Proxima fatia de `M2` deve expandir cobertura para cenarios de compare mode e validar invariantes de restore sob churn de streams.
