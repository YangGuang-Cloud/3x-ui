Managed 3x-ui deployment

Workflow: .github/workflows/deploy-bandwagon.yml
Target: the managed Ubuntu 20.04 amd64 installation of 3x-ui.
Push main or manually dispatch. The build checks frontend types, builds frontend assets,
runs unit/component tests and Go storage/migration tests, and runs Python migration/rollback tests.
It then builds a static musl Linux executable with the Git commit embedded and uploads an artifact.
Only the panel executable and manifest enter the release archive. Xray stays pinned to 26.10.10;
core upgrades need their own compatibility validation and are not performed on a normal push.

Production environment:
DEPLOY_HOST variable: server host/IP.
DEPLOY_SSH_KEY secret: Base64 of a dedicated deployment-only Ed25519 private key.
DEPLOY_KNOWN_HOSTS secret: server host key verified through the trusted console.
Restrict the production environment deployment branch to main.
The deployment account xui-deploy uploads into /var/lib/xui-deploy/incoming.
Its only sudo entry is /usr/local/sbin/3x-ui-deploy with a strictly validated 40-character commit ID.
The authorized SSH public key uses the restrict option. Root-owned install_release.py must be
installed as /usr/local/sbin/3x-ui-deploy; application releases and sudoers stay root-owned.

Server layout:
/opt/3x-ui/runtime -> /opt/3x-ui/releases/<commit>-<unique suffix>
/opt/3x-ui/shared/bin stores the separately managed core and geodata.
/var/lib/3x-ui stores the SQLite database and persistent application data.
/etc/3x-ui/runtime.env stores runtime configuration.
/etc/3x-ui/admin.json contains the server-generated management credential; never upload it.
/var/backups/3x-ui stores private pre-deploy SQLite snapshots.
Service: 3x-ui.service. Management is loopback-only at 127.0.0.1:18083; proxy port is 2087.
The application runs as root as the upstream unit does; deployment access can replace trusted
application code, so the SSH private key and repository write access are important privileges.

Before activating a release, the installer validates archive membership, target architecture,
core version and the executable commit stamp. It stops the service, snapshots the SQLite DB,
then switches the runtime link and starts the release. Failure restores both the prior release
and the SQLite snapshot, including pre-migration schema, then starts the prior version.
Health checks verify the running executable path, panel HTTP response, core child and proxy port.
They do not replace an actual v2rayN test, routing verification or a full restore drill.
Deployments interrupt existing connections briefly. Releases and backups are kept for recovery;
a retention policy must be added before long-term unattended use fills the disk.

The original multi-platform release workflow remains manually runnable and tag-triggered,
but no longer runs on each main push alongside this server-specific deployment.
The upstream CI workflow is preserved.

Tests:
python3 -m unittest discover -s tools/deploy -p 'test_*.py'
python3 -m unittest discover -s tools/legacy-xui -p 'test_*.py'

Status at preparation: migration to the managed service was verified by the user in v2rayN.
Deployment SSH account, GitHub secrets and first CI deployment still need authorization/setup.
