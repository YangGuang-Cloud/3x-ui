import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ClientProviderUsage, type ProviderStatus } from '@/components/bandwagon/BandwagonCard';

const status: ProviderStatus = {
  config: { enabled: true, veid: '1', uploadMbps: 100, downloadMbps: 1000 },
  hasKey: true,
  cycle: { id: 1, start: 1, nextReset: 2, partial: true, quota: 102400, used: 90000, updatedAt: 1 },
  history: [],
  clients: [{ clientId: 7, up: 1024, down: 1024 }],
  total: 2048,
  stale: false,
  awaitingReset: false,
  error: '',
};

describe('separate client usage columns', () => {
  it('shows new ledger usage and divides by plan quota, not server usage or client total', () => {
    render(
      <>
        <ClientProviderUsage status={status} clientId={7} metric="traffic" />
        <ClientProviderUsage status={status} clientId={7} metric="share" />
      </>,
    );
    expect(screen.getByText('2.00 KB')).not.toBeNull();
    expect(screen.getByText('2.00%')).not.toBeNull();
  });

  it('distinguishes an unavailable ledger from a client with zero recorded traffic', () => {
    const { rerender } = render(<ClientProviderUsage clientId={8} metric="traffic" />);
    expect(screen.getByText('—')).not.toBeNull();
    rerender(<ClientProviderUsage status={status} clientId={8} metric="traffic" />);
    expect(screen.getByText('0 B')).not.toBeNull();
  });

  it('calculates each direction against its own capacity and retains idle zero', () => {
    render(
      <ClientProviderUsage
        status={status}
        clientId={7}
        metric="bandwidth"
        speed={{ up: 1250000, down: 0 }}
      />,
    );
    expect(screen.getByText('↑ 10.00% / ↓ 0.00%')).not.toBeNull();
  });

  it('does not invent utilization when bandwidth is unknown', () => {
    render(
      <ClientProviderUsage
        status={{ ...status, config: { ...status.config, uploadMbps: 0, downloadMbps: 0 } }}
        clientId={7}
        metric="bandwidth"
        speed={{ up: 1250000, down: 1250000 }}
      />,
    );
    expect(screen.getByText('↑ — / ↓ —')).not.toBeNull();
  });
});
