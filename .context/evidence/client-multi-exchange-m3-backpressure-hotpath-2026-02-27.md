# Client Multi-Exchange M3 Runtime WS Switch + Web Hot Path (2026-02-27)

## Objective
Evoluir o client web para troca de endpoint WS em runtime (sem reload), mantendo fluxo de reconnect/resubscribe robusto e garantindo renderizacao de todas as widgets online.

## Changes Applied
- `client/web/odin.js`
  - Novo modelo de runtime config com:
    - `buildRuntimeSearchParams`
    - `applyRuntimeConfigWithoutReload`
    - `window.__mr_set_runtime_config_live`
  - Novo override operacional de WS/API key:
    - `wsRuntimeOverride`
    - `window.__mr_switch_ws_runtime(wsUrl, apiKey, options)`
    - `window.__mr_clear_ws_runtime_override()`
  - `ws_connect` agora resolve endpoint efetivo via override em runtime antes de conectar.
  - fechamento/reabertura de socket isolado em helpers (`closeActiveSocket`, `connectSocketUrl`) para reduzir risco de estado stale.
  - painel de conexao atualizado para fluxo live:
    - botao `Apply Live` (sem reload)
    - hint de modo (`url-config` vs `runtime-override`)
    - persistencia de parametros no `history.replaceState`.
- `client/web/index.html`
  - adiciona botao `Apply Live` no painel `Connection Settings`.

## Validation Commands
```bash
make -C client clean
make -C client check-core
make -C client build-wasm
make -C client build-native
make -C client check-core-imports
make up PROCESSOR_REPLICAS=2
make smoke
make -C client check-widgets-online
```

## Results
- `check-core`: `PASS`
- `build-wasm`: `PASS`
- `build-native`: `PASS`
- `check-core-imports`: `PASS`
- `make up PROCESSOR_REPLICAS=2`: stack reconstruida e client web atualizado em `:8090`
- `make smoke`: `PASS`
- `check-widgets-online`: `PASS`
  - coverage:
  - `[soak] ... conn=Connected ... w[t=83 ob=6/12 st=64 hm=79 vp=18 c=2]`

## Playwright MCP (Web Runtime Switch)
- URL base: `http://127.0.0.1:8090/?cb=20260227`
- Acoes validadas:
  - painel `Connection Settings` com `Apply Live` presente
  - troca live para `ws://127.0.0.1:8080/ws?client=live`
  - hint em tela alterado para `mode=runtime-override`
  - URL atualizada com parametros (sem reload completo)
  - reconexao observada + ACKs de subscribe em todos os canais
  - probe de widgets segue > 0 apos troca:
    - `t=256`, `obA=15`, `obB=10`, `st=64`, `hm=128`, `vp=25`, `c=1`

## Notes
- Foi necessario cache-busting (`?cb=20260227`) para garantir carregamento imediato do novo `odin.js` no browser.
- `docker compose ps` direto falhou sem env completo (`TIMESCALE_USER`); `make smoke` confirmou health da stack ativa sem impacto no gate funcional.
