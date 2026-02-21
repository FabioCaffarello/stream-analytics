# ADR-0020 - GitOps Secrets Management

**Status:** Accepted
**Implementation status:** In Progress
**Owner:** Governance Doc-First Maintainer
**Last updated:** 2026-02-21
**Date:** 2026-02-21
**Deciders:** Chief Architect
**Relates to:** ADR-0006 (Storage Hot vs Cold), ADR-0019 (Dual-Database Operational Strategy), `deploy/gitops/`, `deploy/infra/terraform/`

---

## Context

Market Raccoon is transitioning from docker-compose to Kubernetes-native deployment via ArgoCD.
The existing GitOps scaffold at `deploy/gitops/` contains placeholder files but no secrets strategy.
Multiple services require credentials at runtime:

- **TimescaleDB:** user/password for pgx connections.
- **ClickHouse:** user/password for clickhouse-go driver.
- **NATS:** auth tokens (future — currently open in dev).
- **Grafana:** admin user/password.
- **Application services:** database connection strings composed from the above.

Without a secrets management strategy, credentials would either live in plaintext in Git (unacceptable) or require manual `kubectl create secret` operations (fragile, undocumented, un-auditable).

## Decision

### Phase 1: SOPS + ksops via CMP sidecar (current)

Encrypt secrets at rest in Git using [Mozilla SOPS](https://github.com/getsops/sops) with [age](https://github.com/FiloSottile/age) keys. ArgoCD decrypts at sync time via the [ksops](https://github.com/viaduct-ai/kustomize-sops) plugin running in an isolated [Config Management Plugin (CMP) v2 sidecar](https://argo-cd.readthedocs.io/en/stable/operator-manual/config-management-plugins/).

The CMP sidecar approach was chosen over the legacy exec plugin method (init container + global `--enable-alpha-plugins --enable-exec`) because the exec plugin grants arbitrary code execution to any repo with write access. The CMP sidecar isolates plugin execution in a separate container with its own security context, read-only filesystem, and dropped capabilities.

**Key management:**
- One age keypair per environment (local, staging, prod).
- Public keys are committed in `deploy/gitops/.sops.yaml` (creation rules).
- Private keys are stored:
  - **Local:** Terraform `terraform.tfvars` (gitignored) → K8s Secret `sops-age-key` in `argocd` namespace.
  - **CI:** GitHub Secrets (`SOPS_AGE_KEY_LOCAL`, `SOPS_AGE_KEY_STAGING`, `SOPS_AGE_KEY_PROD`).
  - **Production:** Provisioned via secure channel (never in Git).

**Secrets flow:**
```
Developer → sops --encrypt → *.enc.yaml in Git → ArgoCD sync → CMP sidecar (ksops) decrypts → K8s Secret
```

**File convention:**
- Encrypted files: `clusters/<env>/<namespace>/secrets/secrets.enc.yaml`
- SOPS creation rules match `path_regex: clusters/<env>/.*\.enc\.yaml$`

### Phase 2: ESO + Vault (future)

When an external secret store (HashiCorp Vault, AWS Secrets Manager, etc.) is available:

1. Install External Secrets Operator (ESO).
2. Create `ClusterSecretStore` pointing to the vault backend.
3. Replace `*.enc.yaml` files with `ExternalSecret` CRDs referencing vault paths.
4. Zero application manifest changes needed — K8s Secrets are still injected via `envFrom.secretRef`.

## Alternatives Considered

### Sealed Secrets (Bitnami)

- **Rejected.** Re-encryption required on every key rotation. Controller is a runtime dependency (single point of failure). Less portable than SOPS.

### ESO immediately (without Vault)

- **Rejected.** ESO requires an external secret store. Without one, it adds complexity for no benefit. SOPS provides encrypted-in-Git with zero external dependencies.

### Plaintext secrets in private repo

- **Rejected.** Violates defense-in-depth. Credentials visible to anyone with repo read access. No audit trail for secret access.

### Helm Secrets plugin

- **Rejected.** We use Kustomize, not Helm, for application manifests. Would require mixing tooling.

## Consequences

### Positive

- All secrets are version-controlled and auditable (encrypted diffs in Git history).
- No external infrastructure required for Phase 1.
- ArgoCD auto-sync works with encrypted secrets (ksops decrypts transparently).
- Clear upgrade path to Vault/ESO without changing application manifests.
- CI can validate that secrets are actually encrypted (no accidental plaintext commits).

### Negative

- Key rotation requires re-encrypting all secrets for the affected environment.
- Developers need `sops` and `age` CLI tools installed locally.
- Age private key for local env must be shared among team members (acceptable for dev).

### Risks

- If an age private key leaks, all secrets for that environment are compromised. Mitigation: rotate immediately, re-encrypt with new key.
- SOPS/ksops version drift between local tooling and ArgoCD. Mitigation: pin versions in Terraform and document in development workflow.

## Implementation

- `deploy/gitops/.sops.yaml` — SOPS creation rules (age public key per env).
- `deploy/infra/terraform/modules/k8s-bootstrap/sops/` — Terraform module creating `sops-age-key` K8s Secret.
- `deploy/infra/terraform/modules/k8s-bootstrap/argocd/values/argocd.yaml` — CMP sidecar configuration for ksops decryption.
- `.github/workflows/ci-secrets.yml` — CI gate validating encryption.
