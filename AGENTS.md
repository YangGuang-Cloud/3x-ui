# Development and release responsibilities

- The owner's latest workflow: the assistant develops code on `main` or a development branch; the owner performs local acceptance, manually merges into `Release`, triggers deployment by pushing it, and verifies production.
- By default, implement the requested code changes and report what changed. Do not independently run acceptance tests, build/restart the local preview for validation, monitor Actions, deploy, or verify production. Perform these tasks only when the owner specifically requests them. Do not claim unperformed checks passed.
- Ordinary requests to commit/push development changes target `main`, never `Release`. Do not merge into, advance, or push `Release`, dispatch/re-run deployment workflows, or operate the production server as part of routine development.
- After delivery, wait for the owner's local acceptance. The owner handles release promotion and production acceptance; do not automatically continue into those stages.
- If the owner reports a problem and explicitly requests investigation, inspect the relevant evidence within that request's scope. Investigation alone does not authorize a production deployment, rollback, restart, or configuration change.

# Existing deployment safeguards

- `Release` is the case-sensitive production branch. Its push automatically deploys after the workflow checks pass; main/development pushes only build/test.
- This responsibility change does not disable CI, production preflight, fallback checks, health checks, or the installer's automatic rollback. Preserve these safeguards unless the owner explicitly requests a change.
- Keep local databases, binaries, logs, listeners and credentials separate from production. Never use the live service as a development test environment.
- A single-instance cutover interrupts existing connections. Do not promise zero downtime without a separately validated architecture that preserves existing connections.
