# Development and release responsibilities

- The owner's latest workflow: the assistant develops code on `main` or a development branch; the owner performs local acceptance, manually merges into `release`, triggers deployment by pushing it, and verifies production.
- By default, implement the requested code changes and report what changed. Do not independently run acceptance tests, build/restart the local preview for validation, monitor Actions, deploy, or verify production. Perform these tasks only when the owner specifically requests them. Do not claim unperformed checks passed.
- Ordinary requests to commit/push development changes target `main`, never `release`. Do not merge into, advance, or push `release`, dispatch/re-run deployment workflows, or operate the production server as part of routine development.
- After delivery, wait for the owner's local acceptance. The owner handles release promotion and production acceptance; do not automatically continue into those stages.
- If the owner reports a problem and explicitly requests investigation, inspect the relevant evidence within that request's scope. Investigation alone does not authorize a production deployment, rollback, restart, or configuration change.

# Existing deployment safeguards

- `release` is the case-sensitive production branch. Its push packages and deploys; main/development pushes do not run workflows.
- The owner requested only the BandwagonHost deployment workflow. Do not restore upstream CI, automatic test suites, or other workflows without a new request. Keep deployment packaging, production preflight, fallback checks, health checks, and the installer's automatic rollback.
- Keep local databases, binaries, logs, listeners and credentials separate from production. Never use the live service as a development test environment.
- A single-instance cutover interrupts existing connections. Do not promise zero downtime without a separately validated architecture that preserves existing connections.
