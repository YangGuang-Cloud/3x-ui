Managed 3x-ui deployment

Workflow: .github/workflows/deploy-bandwagon.yml
Target: the managed Ubuntu 20.04 amd64 installation of 3x-ui.
Push main only builds and tests. Push Release builds/tests and then automatically deploys
only if all checks pass. There is no manual environment approval, per the owner's final instruction.
Release promotion is a production action; routine development pushes must target main.
Build queues are separate per branch. New development builds may cancel older development
builds, while a Release deployment is never cancelled by a main/development push.
Manual workflow retries must select Release and provide expected_commit equal to the full SHA.
Release was created from c362c86ef0d033cae2ab13b3e10d18d8e68b381c (known-good production).
The initial release-guard setup commit uses [skip ci] only to avoid replacing the live program
while bootstrapping these branches. That exact commit is validated by manual dispatch on main
(build/test only), then synchronized to Release. Normal subsequent Release pushes auto-deploy.
The build checks frontend types, builds frontend assets,
runs unit/component tests and Go storage/migration tests, and runs Python migration/rollback tests.
It then builds a static musl Linux executable with the Git commit embedded and uploads an artifact.
The archive contains the panel, manifest and the SorvoRateV1 bandwidth-aware core built from
the pinned Xray 26.10.10 source. Panel and core switch and roll back together. Protocol/version
upgrades still require separate compatibility validation. See tools/core-rate-limit/README.txt.

Production environment:
DEPLOY_HOST variable: server host/IP.
DEPLOY_SSH_KEY secret: Base64 of a dedicated deployment-only Ed25519 private key.
DEPLOY_KNOWN_HOSTS secret: server host key verified through the trusted console.
Restrict the production environment deployment branch to Release.
No required reviewers or wait timer are configured. Branch restriction to Release and automatic
validation are the release gates; do not re-enable main deployment.
The deployment account xui-deploy uploads into /var/lib/xui-deploy/incoming.
Its only sudo entry is /usr/local/sbin/3x-ui-deploy with a strictly validated 40-character commit ID.
The authorized SSH public key uses the restrict option. Root-owned install_release.py must be
installed as /usr/local/sbin/3x-ui-deploy; application releases and sudoers stay root-owned.

Server layout:
/opt/3x-ui/runtime -> /opt/3x-ui/releases/<commit>-<unique suffix>
/opt/3x-ui/shared/bin stores geodata and a core symlink through runtime/xray-linux-amd64.
/var/lib/3x-ui stores the SQLite database and persistent application data.
/etc/3x-ui/runtime.env stores runtime configuration.
/etc/3x-ui/admin.json contains the server-generated management credential; never upload it.
/var/backups/3x-ui stores private pre-deploy SQLite snapshots.
Service: 3x-ui.service. Management is loopback-only at 127.0.0.1:18083; proxy port is 2087.
The application runs as root as the upstream unit does; deployment access can replace trusted
application code, so the SSH private key and repository write access are important privileges.

Before any service stop, the installer verifies the live baseline can forward real HTTPS requests,
loads the production configuration with the candidate core in test mode, and starts a candidate
core on separate loopback ports with private config/log/rate copies. A private probe client uses
actual VLESS credentials and pins the production certificate, then forwards HTTPS through every
VLESS inbound. Production routing, outbounds and geodata remain in the isolated configuration.
The current supported topology is VLESS TCP/raw TLS and WebSocket (TLS or nginx-terminated),
plus internal API/metrics. Unknown listeners/transports fail closed and require separate staging.
Only listener addresses/ports and logs are changed in the shadow copy; no live service, database,
public listener or runtime link is changed by preflight. Live config changes invalidate validation.
The installer refuses to convert legacy core layouts during a release.

After all preflight checks pass, it stops the service, snapshots SQLite, switches runtime and
starts the candidate. Acceptance requires the right executable, panel HTTP, core child, proxy port
and actual proxy HTTP(S) requests. Failure restores the previous runtime and SQLite schema/data,
then checks real traffic again. An unhealthy rollback is an explicit deployment failure requiring
recovery, never reported as a successful rollback. This does not validate the whole candidate panel
in an isolated database, nor the public nginx/Cloudflare path; those still require candidate-specific
review and external client verification.
Deployments interrupt existing connections briefly. Releases and backups are kept for recovery;
a retention policy must be added before long-term unattended use fills the disk.

Independent fallback is installed as xray-fallback.service on TCP 24444. Immediately before
cutover, xray-fallback-check must pass a real HTTPS proxy request; failure aborts before stopping
3x-ui. The fallback executable/configuration live outside /opt/3x-ui and are never updated or
restarted by this installer. See ../fallback/README.txt. Client-side switching is manual.

The original multi-platform release workflow remains manually runnable and tag-triggered,
but no longer runs on each main push alongside this server-specific deployment.
The upstream CI workflow is preserved.

Tests:
python3 -m unittest discover -s tools/deploy -p 'test_*.py'
python3 tools/deploy/verify_preflight.py <path-to-built-core>
python3 -m unittest discover -s tools/legacy-xui -p 'test_*.py'

The current single-instance service still reconnects clients during an approved cutover.
Continuous existing connections require a separately designed and tested dual-instance or
dual-server traffic-switching and draining architecture.
