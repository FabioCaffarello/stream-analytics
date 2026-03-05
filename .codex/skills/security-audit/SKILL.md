---
name: Security Audit
description: Security review checklist for code and infrastructure
phases: [R, V]
---

# Security Audit Skill

## Security Checklist
- Validate external input at boundaries (HTTP and ws ingestion paths).
- Ensure no secrets are hardcoded in code or docs.
- Confirm dependency scanning path is active (`make vuln`, CI with `VULN_REQUIRED=true`).
- Review error messages to avoid leaking sensitive internals.

## Common Vulnerabilities To Check
- Input validation gaps and unsafe parsing assumptions.
- Concurrency hazards causing undefined behavior under load.
- Missing timeouts/cancellation on long-running operations.
- Dependency drift without vulnerability recheck.

## Auth/Authz Note
Current runtime endpoints should remain minimal and controlled; if privileged operations expand, require explicit authz boundaries and audit logs.

## Verification Commands
- `make lint`
- `make test`
- `make vuln`
- `make ci VULN_REQUIRED=true`