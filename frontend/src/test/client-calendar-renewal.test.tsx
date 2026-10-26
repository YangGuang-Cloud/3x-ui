import { expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

it('retires lifecycle controls and saves a legacy client without reactivating its old schedule', async () => {
  const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, '', {}));
  const get = vi
    .spyOn(HttpUtil, 'get')
    .mockResolvedValue(new Msg(true, '', { uploadMbps: 0, downloadMbps: 0, supported: true }));
  const save = vi.fn().mockResolvedValue(new Msg(true, '', null));
  try {
    renderWithProviders(
      <ClientFormModal
        open
        mode="edit"
        client={{
          email: 'legacy@example.com',
          uuid: '11111111-1111-1111-1111-111111111111',
          subId: 'legacy',
          enable: true,
          expiryTime: 1893427200000,
          resetDay: 1,
          reset: 7,
          resetMax: 3,
          trafficReset: 'monthly',
        }}
        attachedIds={[1]}
        inbounds={[{ id: 1, protocol: 'vless', tag: 'local' }]}
        save={save}
        onOpenChange={() => {}}
      />,
    );
    for (const label of ['Auto renewal', 'Expiration', 'Start after first use', 'Traffic Reset']) {
      expect(screen.queryByLabelText(label)).toBeNull();
    }
    const button = document.querySelector('.ant-modal-footer .ant-btn-primary');
    expect(button).not.toBeNull();
    await waitFor(() => expect(button!.hasAttribute('disabled')).toBe(false));
    fireEvent.click(button!);
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        expect.objectContaining({
          expiryTime: 0,
          reset: 0,
          resetDay: 0,
          resetWeekday: 0,
          resetMax: 0,
          trafficReset: 'never',
          enable: true,
        }),
        expect.anything(),
      ),
    );
    expect(post.mock.calls.some(([url]) => url === '/panel/api/clients/renewalPreview')).toBe(
      false,
    );
  } finally {
    post.mockRestore();
    get.mockRestore();
  }
});
