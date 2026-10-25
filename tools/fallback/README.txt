Independent same-server fallback

Service: xray-fallback.service, TCP 24444, VLESS + REALITY.
Independent regular executable: /opt/xray-fallback/xray (copied from the known-good core).
Configuration and credentials: /etc/xray-fallback. Never commit client files or private keys.
No shared database, core symlink, geodata, nginx, certificates or panel dependencies.
Dedicated unprivileged user, read-only filesystem, 96 MiB Go memory target and 192 MiB memory ceiling, automatic restart
and boot start. The main systemd service has no dependency on this service.
REALITY handshake target: www.apple.com:443; authentication uses a separate generated key,
UUID and short ID. Import the client-url.txt into a REALITY-capable client in advance.
Use the fallback manually when the main node is unavailable; subscription refresh is not required.

One-time root install: python3 install.py. It refuses to overwrite existing fallback files.
Install check.py as root-owned /usr/local/sbin/xray-fallback-check (0755).
Validate the systemd unit, then systemctl daemon-reload and enable --now xray-fallback.
The health command forwards a real HTTPS request through a temporary loopback client.
It does not restart, update or reconfigure either server service. Validate the public port
from an external computer separately; the server check cannot prove public reachability.

The deployment entry point calls this health check immediately before stopping the main
service. Missing helper, stopped service, failed traffic or timeout aborts before cutover.
Release archives and rollback touch /opt/3x-ui and /var/lib/3x-ui only; never add fallback
updates to that path. Updating the fallback is a separate maintenance operation after verifying
that the main service works. Do not update both at once.

Shared limits: same VPS, IP, upstream network, firewall and traffic quota. Host reboot,
provider outage or shared firewall changes can interrupt both. The main panel runs as root,
so this is operational separation, not a security boundary against malicious main code.
Fallback traffic is included in provider totals but not attributed to main-panel client records.
Do not stop the main service solely to demonstrate isolation on a live user connection.

Validated 2026-10-26 (Asia/Shanghai): server-side HTTPS check passed; three requests from
the Mac directly through public TCP 24444 returned HTTP 200 (example.com and Cloudflare).
Imported Sorvo-独立备用 into v2rayN as a standalone node, outside subscription groups;
its real-connection delay test returned 197 ms. Main node remains selected for active traffic.
Both existing main VLESS inbounds passed real proxy requests; main PID stayed 1939358.
Final fallback memory was approximately 8 MiB, zero restarts after the final configuration.
Vision was excluded after it triggered the fallback's memory ceiling in initial testing.
Do not enable Vision without a separate investigation and validation. Current plain VLESS
REALITY tests passed. Fourteen deployment unit tests passed, including failed fallback guards.
No live main-service outage was induced, no Release push or application upgrade performed.
