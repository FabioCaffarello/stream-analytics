# System Invariants

These rules are never violated.

---

## Determinism

Same input → same output.

---

## Replayability

All pipelines must be reconstructable.

---

## Domain Isolation

Business rules never live in actors.

---

## Replaceable Infrastructure

Adapters are swappable.

---

## Evidence Over Opinion

Insights must be explainable.


---

# O que fazer AGORA (ordem perfeita)

Crie:

```

docs/
 ├── adrs/
 ├── architecture/
 │     README.md
 │     ingestion.md
 │     insights.md
 │     system-invariants.md
 └── contracts/
       event-bus.md

```

Commit gigante inicial:

👉 `feat(architecture): foundational system design`

---

# Próximo passo que EU recomendo fortemente

Depois desses docs…

Crie UM documento que muda o destino do produto:

## 👉 `docs/architecture/moat.md`

Explica:

> por que esse sistema será difícil de copiar.

Isso guia TODAS as decisões futuras.

Posso escrever ele para você — e honestamente, é um dos documentos mais valiosos que uma startup deep-tech pode ter.

---

Se quiser, no próximo passo eu posso te entregar:

🔥 o blueprint do **runtime de actors ideal para market data**
🔥 como estruturar o **Instrument Actor** (talvez o actor mais importante do sistema)
🔥 o erro arquitetural que destrói 80% das plataformas de dados
🔥 como preparar o sistema para **10x volume** sem reescrever

Só falar — porque agora você está montando fundação de empresa, não só repo.
