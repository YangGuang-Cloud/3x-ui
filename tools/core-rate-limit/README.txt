Per-account bandwidth extension

User-facing settings: Clients -> edit -> Basics -> independent upload/download Mbps.
Client information and bandwidth limits share the bottom Save button.
0 means unlimited. All connections of the same account on this panel share each directional
bucket, including different local VLESS inbounds. 8 Mbps = 1 MB/s. A 64 KiB burst is allowed.
Saving a changed limit restarts the local core and reconnects its active sessions.
The first supported deployment is local VLESS (including TLS/Vision, WebSocket and UDP).
Remote nodes and other protocols are rejected by the settings API, not silently presented as limited.
New and existing accounts default to unlimited. Limits survive account renames and database backups.
Individual client export/import does not currently transfer these panel-local policies.

The stock Xray executable has no matching feature. build.py takes the exact module pinned by
this repository's go.mod, copies it to a temporary directory and checks three dispatcher anchors
before injecting dispatcher.go. It changes neither the module cache nor the repository dependency
pins. The separately built core advertises SorvoRateV1 in its build string; the panel rejects
positive limits when the actual executable lacks that capability.
Limits are generated from canonical client IDs into a private, atomically written sidecar file.
A rate change is not hot-applied: the normal restart path reloads the account buckets. This also
prevents an existing unlimited connection from escaping a newly enabled limit. Limited sessions
cannot use the zero-copy splice path. Timeout reads, cancellation, UDP packet boundaries and
stream close/interrupt are retained.

Build and test from the repository root:
  python3 tools/core-rate-limit/build.py --output /tmp/xray-rate-test
  go test ./tools/core-rate-limit
  python3 tools/core-rate-limit/verify.py /tmp/xray-rate-test

The integration test runs two isolated loopback cores, uses a temporary pinned TLS certificate,
and measures simultaneous Vision + WebSocket upload/download against a shared 2 Mbps account.
A separate unlimited account and UDP packet integrity are checked. Its freedom allow rule is
restricted to loopback test traffic and is never copied into production routing.

Deployment bundles this core beside the panel in each release. The shared core path resolves
through the runtime link, so a failed deployment rolls back the executable, core and SQLite snapshot
as a unit. Before the first such release, install the updated tools/deploy/install_release.py as
the existing root-owned /usr/local/sbin/3x-ui-deploy. It preserves the old core in the previous release
before switching the shared core path to a managed symlink. Existing certificate and geodata paths
stay under shared/bin. Never replace the managed core using the upstream self-update button.

Upstream core upgrades require reviewing and rebuilding this small dispatcher extension, rerunning
the integration test and updating the coordinated deployment version pin. An anchor mismatch fails
the build instead of applying an unreviewed patch.
