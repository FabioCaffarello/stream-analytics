"""
MkDocs gen-files hook for Market Raccoon.

Generates into MkDocs' virtual filesystem (does not touch real docs/ files):
  SUMMARY.md                       literate-nav navigation with T1/T2/T3/T4 tiers
  contracts/subject-registry.md    Markdown table rendered from subject-registry.yaml
"""
from __future__ import annotations

from pathlib import Path

import mkdocs_gen_files
import yaml

REPO_ROOT = Path(__file__).parent.parent.parent
DOCS_ROOT = REPO_ROOT / "docs"
REGISTRY_PATH = DOCS_ROOT / "contracts" / "subject-registry.yaml"


def generate_subject_registry_page() -> str:
    with REGISTRY_PATH.open() as fh:
        data = yaml.safe_load(fh)

    lines: list[str] = [
        "# Subject Registry\n\n",
        "> Auto-generated from `docs/contracts/subject-registry.yaml`  \n",
        f"> Version: `{data.get('version', '?')}` — Last updated: `{data.get('last_updated', '?')}`\n\n",
        f"**Subject format:** `{data.get('canonical_subject_format', '')}`\n\n",
        f"**Governance:** {data.get('governance', {}).get('schema_authority_rule', '')}\n\n",
        "---\n\n",
        "| Subject ID | Pattern | Owner BC | Producer BC | Schema Authority | Consumers | Status |\n",
        "|------------|---------|----------|-------------|-----------------|-----------|--------|\n",
    ]

    by_root: dict[str, list[dict]] = {}
    for subj in data.get("subjects", []):
        by_root.setdefault(subj.get("root", "unknown"), []).append(subj)

    for root in sorted(by_root):
        for subj in by_root[root]:
            consumers = ", ".join(f"`{c}`" for c in subj.get("consumer_bcs", []))
            lines.append(
                f"| `{subj['id']}` "
                f"| `{subj.get('pattern', '')}` "
                f"| `{subj.get('owner_bc', '')}` "
                f"| `{subj.get('producer_bc', '')}` "
                f"| `{subj.get('schema_authority_bc', '')}` "
                f"| {consumers} "
                f"| {subj.get('status', '')} |\n"
            )

    findings = data.get("findings", {})
    naming_issues = findings.get("naming_inconsistencies", [])
    if naming_issues:
        lines.append("\n## Known Findings\n\n")
        for item in naming_issues:
            lines.append(f"- **{item.get('issue', '')}** — {item.get('resolution', '')}\n")

    return "".join(lines)


SUMMARY = """\
<!--nav-->
* [Home](README.md)
* Platform
    * [Overview](architecture/README.md)
    * [Subsystems](architecture/subsystems.md)
    * [System Invariants](architecture/system-invariants.md)
    * [Sequencing Model](architecture/sequencing-model.md)
* Architecture Diagrams
    * [C4 System Context](architecture/diagrams/c4-context.md)
    * [C4 Service Map](architecture/diagrams/c4-containers.md)
    * [Live Ingestion Flow](architecture/diagrams/sequence-live-ingestion.md)
    * [Storage Federation](architecture/diagrams/sequence-storage-federation.md)
    * [Exchange Recovery](architecture/diagrams/sequence-exchange-recovery.md)
    * [Actor Supervision](architecture/diagrams/actor-supervision-tree.md)
    * [Client Session Protocol](architecture/diagrams/sequence-client-session.md)
    * [LEL Evidence Detection](architecture/diagrams/sequence-evidence-lel.md)
* Analytics
    * [Pipeline Overview](architecture/analytics-pipeline.md)
    * [C4 Analytics Profile](architecture/diagrams/c4-analytics.md)
    * [Analytics Flow](architecture/diagrams/sequence-analytics-pipeline.md)
* Market Data
    * [Candle Aggregation](architecture/candle-aggregation.md)
    * [Stats Aggregation](architecture/stats-aggregation.md)
    * [Orderbook](architecture/orderbook.md)
    * [Heatmap](architecture/heatmap.md)
    * [Volume Profiles](architecture/volume-profiles.md)
    * [Liquidations & Mark Price](architecture/liquidations-markprice.md)
    * [Insights](architecture/insights.md)
* Data Contracts
    * [Event Bus](contracts/event-bus.md)
    * [Delivery WebSocket](contracts/delivery-ws.md)
    * [Canonical Market Model](contracts/canonical-market-model.md)
    * [Liquidity Evidence Layer](contracts/liquidity-evidence-layer.md)
    * [Boundedness Matrix](contracts/boundedness-matrix.md)
    * [Subject Registry](contracts/subject-registry.md)
* Client Cockpit
    * [Architecture](client/client-architecture.md)
    * [Layer Architecture](client/layer-architecture.md)
    * [Memory Ownership](client/client-memory-ownership-rules.md)
* Operations
    * [Local Dev](local-dev.md)
    * [Storage](architecture/storage.md)
    * [Metrics Catalogue](architecture/metrics-catalogue.md)
    * [Sharding](operations/sharding.md)
    * [Emulator](operations/emulator.md)
    * [Validator](operations/validator.md)
"""


with mkdocs_gen_files.open("SUMMARY.md", "w") as fh:
    fh.write(SUMMARY)

with mkdocs_gen_files.open("contracts/subject-registry.md", "w") as fh:
    fh.write(generate_subject_registry_page())
