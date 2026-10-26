import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from './test-utils';
import { ClientPresenceMetric, type ProviderStatus } from '@/components/bandwagon/BandwagonCard';
const status = {
  config: { enabled: true },
  cycle: { id: 1 },
  presenceAvailable: true,
  presence: [{ clientId: 1, onlineSeconds: 3661, onlineIPs: 2 }],
} as ProviderStatus;
describe('cycle online metrics', () => {
  it('formats accumulated time above an hour', () => {
    renderWithProviders(<ClientPresenceMetric status={status} clientId={1} metric="duration" />);
    expect(screen.getByText('1小时 1分 1秒')).toBeTruthy();
  });
  it('shows IP count separately from duration', () => {
    renderWithProviders(<ClientPresenceMetric status={status} clientId={1} metric="ips" />);
    expect(screen.getByText('2')).toBeTruthy();
  });
  it('does not claim zero IPs when detection is unavailable', () => {
    renderWithProviders(
      <ClientPresenceMetric
        status={{ ...status, presenceAvailable: false }}
        clientId={1}
        metric="ips"
      />,
    );
    expect(screen.getByText('—')).toBeTruthy();
  });
  it('starts a new cycle at zero', () => {
    renderWithProviders(
      <ClientPresenceMetric status={{ ...status, presence: [] }} clientId={1} metric="duration" />,
    );
    expect(screen.getByText('0小时 0分 0秒')).toBeTruthy();
  });
});
