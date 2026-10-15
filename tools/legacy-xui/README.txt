Legacy x-ui staging migration

Scope: the inspected x-ui 0.3.4.4 SQLite schema, exactly one VLESS TCP/TLS inbound.
Run on the server with a private SQLite backup, never a production database opened for writing:

python3 tools/legacy-xui/export.py --backup /root/private-backup/x-ui.db --test-port 24443 --output /root/private-staging-export

The source is opened read-only. The output directory must not exist and is created with mode 700;
JSON files contain node credentials and are created under umask 077. Keep them outside Git.
The tool stops for unmatched traffic, duplicate identities, unsupported flows/protocols/transports,
or old IP/reset policies requiring manual mapping. It only prepares JSON; it does not run a server.

Apply each inbounds.json object to a fresh, separate panel through POST /panel/api/inbounds/add
with authenticated session and CSRF token. Do not use /import: that handler enables every
clientStats entry, which would change disabled clients. Verify the resulting database and core.
No plain SQLite database import is supported by upstream.

The payload preserves node identities, TLS references, enable flags, client traffic/quota/expiry.
Only the proxy port changes for staging. Global DNS/routing/panel settings are not exported.
Certificate paths reference existing server files; certificates are not copied by this tool.
Use a fresh backup before final cutover so staging traffic/old snapshot counters are not promoted.
A positive staging test does not authorize replacing the production service.

Tests:
python3 -m unittest discover -s tools/legacy-xui -p 'test_*.py'

Current staging:
3x-ui 3.10.2 / Xray 26.10.10, same-version official Linux release used for initial compatibility test.
The user's Fork has been built locally for macOS; a private-fork Linux build/deploy pipeline is pending.
Separate candidate/data under /opt/3x-ui-test; transient systemd unit 3x-ui-test.
Panel: 127.0.0.1:18083. Xray API: 127.0.0.1:26279. Metrics: 127.0.0.1:31111.
Proxy: TCP 24443. No persistent firewall policy was changed.
Old x-ui.service and its database/program were not modified.

Verified on 2026-10-15:
- Three real client identities, enable flags, traffic/quota/expiry matched the backup before probe.
- TLS leaf certificate matched the existing certificate.
- Server-side Xray client -> staging VLESS -> HTTPS destination returned HTTP 200.
- Mac -> public TCP 24443 TLS handshake succeeded (TLS 1.3).
- User's v2rayN external proxy test is pending.

The internal probe uses the current core's pinnedPeerCertSha256 option, not removed allowInsecure.
The loopback management account was generated server-side and stays in /opt/3x-ui-test/admin.json.
No password/UUID/certificate private key was transferred into this repository.
