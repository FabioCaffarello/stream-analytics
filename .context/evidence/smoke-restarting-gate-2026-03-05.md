## Smoke Gate: Restarting Detection
Date: 2026-03-05

### Change
- Arquivo alterado: `scripts/test/util/smoke-compose.sh`
- Regra adicionada: falhar `make smoke` quando qualquer serviço do profile `core` estiver com status `Restarting`.

### Verification
Comando executado:
- `make smoke`

Resultado observado:
- gate falhou imediatamente com:
- `compose-signals-1 ... Restarting (0)`
- `compose-strategist-1 ... Restarting (0)`

Conclusão:
- falso-verde eliminado para este cenário; crash loop de serviços core agora bloqueia o smoke gate.
