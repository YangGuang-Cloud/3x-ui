# Production release policy

- Develop and push ordinary changes on `main` or development branches. These actions must never deploy production.
- `Release` is the case-sensitive production release branch. It was created from known-good production commit `c362c86ef0d033cae2ab13b3e10d18d8e68b381c`.
- The owner's latest instruction is automatic deployment on a `Release` push, with no manual GitHub approval. Advancing/pushing `Release` is therefore a production deployment action. Ordinary requests to commit/push development changes target `main`, not `Release`.
- Do not advance or push `Release` until the owner explicitly says to start deployment. Development requests authorize local changes and tests only; report readiness and wait for that instruction.
- Build and validate before promoting a candidate to `Release`. Report the exact candidate, tests, migration/core changes and expected reconnection. Never merge unfinished development to `Release` merely to test it.
- The production environment must accept only `Release`. Main/development pushes only build/test. Do not bypass failed candidate checks, production-config preflight or rollback verification.
- Keep validation copies, databases, logs, listeners and credentials separate from production. Never test an unvalidated release by stopping or replacing the live service.
- On failure, restore the previous release and SQLite snapshot and verify real proxy traffic after rollback. A running panel or open TCP port alone is insufficient.
- A single-instance cutover interrupts existing connections. Do not promise zero downtime without a separately validated architecture that preserves existing connections.
