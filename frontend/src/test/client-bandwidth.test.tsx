import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ClientBandwidthModal from '@/pages/clients/ClientBandwidthModal';
import { HttpUtil, Msg } from '@/utils';

afterEach(() => vi.restoreAllMocks());
describe('client bandwidth settings', () => {
  it('loads and saves independent upload/download caps for the selected account', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(
      new Msg(true, '', { uploadMbps: 0, downloadMbps: 0, supported: true }),
    );
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, ''));
    const close = vi.fn();
    render(<ClientBandwidthModal email="a@test" onClose={close} />);
    fireEvent.change(await screen.findByRole('spinbutton', { name: 'Upload limit' }), {
      target: { value: '2' },
    });
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Download limit' }), {
      target: { value: '10' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/panel/api/clients/a%40test/bandwidth', {
        uploadMbps: 2,
        downloadMbps: 10,
      }),
    );
    expect(close).toHaveBeenCalledOnce();
  });
  it('does not enable limits on a core without the required capability', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(
      new Msg(true, '', { uploadMbps: 0, downloadMbps: 0, supported: false }),
    );
    const post = vi.spyOn(HttpUtil, 'post');
    render(<ClientBandwidthModal email="a@test" onClose={vi.fn()} />);
    fireEvent.change(await screen.findByRole('spinbutton', { name: 'Download limit' }), {
      target: { value: '10' },
    });
    const save = screen.getByRole('button', { name: 'Save' });
    expect(save.hasAttribute('disabled')).toBe(true);
    fireEvent.click(save);
    expect(post).not.toHaveBeenCalled();
  });
  it('keeps the dialog open when the server cannot apply the limit', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(
      new Msg(true, '', { uploadMbps: 0, downloadMbps: 0, supported: true }),
    );
    const post = vi
      .spyOn(HttpUtil, 'post')
      .mockResolvedValue(new Msg(false, 'Core restart failed'));
    const close = vi.fn();
    render(<ClientBandwidthModal email="a@test" onClose={close} />);
    await screen.findByRole('spinbutton', { name: 'Download limit' });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(post).toHaveBeenCalledOnce());
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(close).not.toHaveBeenCalled();
  });
});
