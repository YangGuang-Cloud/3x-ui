"""Read a legacy backup and prepare VLESS payloads for a separate test panel."""
import argparse
import json
import os
import sqlite3
import uuid
from pathlib import Path


def convert(path, test_port):
    if not 1024 <= test_port <= 65535:
        raise ValueError('Test port must be between 1024 and 65535')
    path = Path(path).resolve(strict=True)
    c = sqlite3.connect(path.as_uri() + '?mode=ro', uri=True)
    c.row_factory = sqlite3.Row
    try:
        rows = [dict(r) for r in c.execute('SELECT * FROM inbounds ORDER BY id')]
        stats = [dict(r) for r in c.execute('SELECT * FROM client_infos')]
    finally:
        c.close()
    if len(rows) != 1:
        raise ValueError('This migration requires exactly one legacy inbound')
    row = rows[0]
    if row['protocol'] != 'vless':
        raise ValueError('Only VLESS is supported by this migration')
    if row['port'] == test_port:
        raise ValueError('Test port must be different from the original port')
    if any(row.get(key, 0) for key in ('autoreset', 'ip_alert', 'ip_limit')):
        raise ValueError('Legacy reset/IP policies require manual mapping')
    settings = json.loads(row['settings'])
    stream = json.loads(row['stream_settings'])
    if stream.get('network', 'tcp') != 'tcp' or stream.get('security') != 'tls':
        raise ValueError('Only TCP/TLS transport is supported by this migration')
    clients = settings.get('clients')
    if not isinstance(clients, list) or not clients:
        raise ValueError('No legacy clients found')
    by_email = {}
    for stat in stats:
        email = stat['email']
        if stat['inbound_id'] != row['id'] or email in by_email:
            raise ValueError('Duplicate or unmatched client traffic requires manual review')
        by_email[email] = stat
    seen = set()
    identities = set()
    client_stats = []
    for client in clients:
        try:
            identity = str(uuid.UUID(client.get('id', '')))
        except (ValueError, AttributeError):
            raise ValueError('Invalid legacy client identity') from None
        email = client.get('email', '')
        if not email or email.casefold() in seen or identity in identities:
            raise ValueError('Missing or duplicate client identity/email requires manual review')
        if client.get('flow', '') not in ('', 'xtls-rprx-vision'):
            raise ValueError('Legacy flow requires manual compatibility review')
        seen.add(email.casefold())
        identities.add(identity)
        stat = by_email.pop(email, None)
        client['enable'] = bool(stat['enable']) if stat else bool(client.get('enable', True))
        client['totalGB'] = stat['total'] if stat else client.get('totalGB', 0)
        client['expiryTime'] = stat['expiry_time'] if stat else client.get('expiryTime', 0)
        if stat:
            client_stats.append({'id': 0, 'inboundId': 0, 'email': email,
                                 'enable': client['enable'], 'up': stat['up'], 'down': stat['down'],
                                 'total': stat['total'], 'expiryTime': stat['expiry_time']})
    if by_email:
        raise ValueError('There are unmatched client traffic records')
    payload = {key: row[key] for key in ('up', 'down', 'total', 'remark', 'listen', 'protocol', 'tag', 'sniffing')}
    payload.update(id=0, port=test_port, enable=bool(row['enable']), expiryTime=row['expiry_time'],
                   settings=json.dumps(settings), streamSettings=json.dumps(stream), clientStats=client_stats)
    report = {'source': 'legacy-x-ui-0.3.4.4', 'inboundCount': 1, 'clientCount': len(clients),
              'trafficRecords': len(client_stats), 'testPort': test_port,
              'targetAPI': '/panel/api/inbounds/add', 'liveConnectionVerified': False}
    return [payload], report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--backup', type=Path, required=True)
    parser.add_argument('--test-port', type=int, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        payloads, report = convert(args.backup, args.test_port)
    except (ValueError, OSError, sqlite3.Error, KeyError, TypeError):
        parser.exit(1, 'Migration stopped: incompatible or unreadable backup; no output written.\n')
    os.umask(0o077)
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    (args.output / 'inbounds.json').write_text(json.dumps(payloads, indent=2))
    (args.output / 'report.json').write_text(json.dumps(report, indent=2))
    print(json.dumps(report))


if __name__ == '__main__':
    main()
