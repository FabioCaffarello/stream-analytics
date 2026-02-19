# Month 1 Baseline (Performance + Resilience)

## Objetivo
Estabelecer um harness reproduzível para medir hot-path sem quebrar determinismo/replay.

## Pré-requisitos
- Workspace pronto com dependências (`npm install` quando aplicável ao fluxo local).
- Go toolchain disponível.
- Opcional para comparação estatística: `benchstat`.

## Execução padrão
Rodar o alvo consolidado de benchmark hot-path:

```bash
make bench-hotpath
```

Rodar benchmark de forma direta com configuração customizada:

```bash
go test -run=^$ -bench=HotPath -benchmem ./internal/shared/codec ./internal/shared/policykit
```

## Coleta de CPU profile
Gerar profile CPU durante benchmark:

```bash
go test -run=^$ -bench=HotPath -benchmem -cpuprofile .context/evidence/bench-hotpath-cpu.prof ./internal/shared/codec ./internal/shared/policykit
```

Inspecionar:

```bash
go tool pprof .context/evidence/bench-hotpath-cpu.prof
```

## Coleta de alloc/memory profile
Gerar profile de alocação:

```bash
go test -run=^$ -bench=HotPath -benchmem -memprofile .context/evidence/bench-hotpath-mem.prof ./internal/shared/codec ./internal/shared/policykit
```

Inspecionar:

```bash
go tool pprof .context/evidence/bench-hotpath-mem.prof
```

## Comparação entre runs
Salvar saídas completas de duas execuções:

```bash
go test -run=^$ -bench=HotPath -benchmem ./internal/shared/codec ./internal/shared/policykit > .context/evidence/bench-old.txt
go test -run=^$ -bench=HotPath -benchmem ./internal/shared/codec ./internal/shared/policykit > .context/evidence/bench-new.txt
```

Comparar com `benchstat` (opcional, não obrigatório em CI):

```bash
benchstat .context/evidence/bench-old.txt .context/evidence/bench-new.txt
```

## Registro de evidência
Guardar artefatos em `.context/evidence/`:
- output bruto do benchmark
- `cpuprofile`
- `memprofile`
- diff `benchstat` quando aplicável

Não registrar números fixos neste documento; apenas workflow de coleta.
