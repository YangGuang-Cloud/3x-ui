import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import ClientBulkAddModal from '@/pages/clients/ClientBulkAddModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';
const { bulkCreate } = vi.hoisted(() => ({ bulkCreate: vi.fn() }));
vi.mock('@/hooks/useClients', () => ({ useClients: () => ({ bulkCreate }) }));
it('creates clients without the retired expiration and renewal schedules', async () => {
  bulkCreate.mockResolvedValue(new Msg(true, '', { created: 1, skipped: [] }));
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', {}));
  try {
    renderWithProviders(
      <ClientBulkAddModal
        open
        inbounds={[{ id: 1, protocol: 'vless', tag: 'local' }]}
        onOpenChange={() => {}}
      />,
    );
    expect(screen.queryByLabelText('Auto renewal')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Select all' }));
    fireEvent.click(document.querySelector('.ant-modal-footer .ant-btn-primary')!);
    await waitFor(() =>
      expect(bulkCreate).toHaveBeenCalledWith([
        expect.objectContaining({
          client: expect.objectContaining({
            expiryTime: 0,
            reset: 0,
            resetDay: 0,
            resetWeekday: 0,
            trafficReset: 'never',
          }),
        }),
      ]),
    );
    expect(post.mock.calls.some(([url]) => url === '/panel/api/clients/renewalPreview')).toBe(
      false,
    );
  } finally {
    post.mockRestore();
  }
});
