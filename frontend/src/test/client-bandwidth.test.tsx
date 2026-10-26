import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import { HttpUtil, Msg } from '@/utils';
import { renderWithProviders } from './test-utils';

afterEach(() => vi.restoreAllMocks());
const defaults = { uploadMbps: 2, downloadMbps: 10, supported: true };
function mount(save = vi.fn().mockResolvedValue(new Msg(true, '')), close = vi.fn()) {
  renderWithProviders(
    <ClientFormModal
      open
      mode="edit"
      client={{
        email: 'a@test',
        enable: true,
        uuid: '11111111-1111-1111-1111-111111111111',
        subId: 'test',
      }}
      inbounds={[]}
      save={save}
      onOpenChange={close}
    />,
  );
  return { save, close };
}
async function changeDownload(n: string) {
  fireEvent.change(await screen.findByRole('spinbutton', { name: 'Download limit' }), {
    target: { value: n },
  });
}
const submit = () => fireEvent.click(screen.getByRole('button', { name: /Save/ }));

describe('unified client save', () => {
  it('saves information then changed bandwidth through the sole footer Save button', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', defaults));
    const calls: string[] = [];
    const post = vi.spyOn(HttpUtil, 'post').mockImplementation(async (url) => {
      if (url.endsWith('/bandwidth')) calls.push('bandwidth');
      return new Msg(true, '');
    });
    const save = vi.fn(async () => {
      calls.push('client');
      return new Msg(true, '');
    });
    const { close } = mount(save);
    await changeDownload('20');
    expect(screen.queryByRole('tab', { name: 'Client bandwidth limit' })).toBeNull();
    expect(screen.getAllByRole('button', { name: /Save/ })).toHaveLength(1);
    submit();
    await waitFor(() => expect(close).toHaveBeenCalledWith(false));
    expect(calls).toEqual(['client', 'bandwidth']);
    expect(post).toHaveBeenCalledWith(
      '/panel/api/clients/a%40test/bandwidth',
      { uploadMbps: 2, downloadMbps: 20 },
      { silent: true, headers: { 'Content-Type': 'application/json' } },
    );
  });
  it('does not apply bandwidth if client information fails', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', defaults));
    const post = vi.spyOn(HttpUtil, 'post');
    const { close } = mount(vi.fn().mockResolvedValue(new Msg(false, 'Client update failed')));
    await changeDownload('20');
    post.mockClear();
    submit();
    expect(await screen.findByText('Client update failed')).toBeTruthy();
    expect(post).not.toHaveBeenCalled();
    expect(close).not.toHaveBeenCalled();
  });
  it('keeps edited limits after apply failure and retries against a renamed client', async () => {
    let failedOnce = false;
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', defaults));
    const post = vi.spyOn(HttpUtil, 'post').mockImplementation(async (url) => {
      if (url.endsWith('/bandwidth') && !failedOnce) {
        failedOnce = true;
        return new Msg(false, 'Core restart failed');
      }
      return new Msg(true, '');
    });
    const { save, close } = mount();
    await changeDownload('20');
    fireEvent.change(screen.getByPlaceholderText('Email'), { target: { value: 'renamed@test' } });
    submit();
    expect(
      await screen.findByText(
        /Client information was saved, but bandwidth settings were not confirmed/,
      ),
    ).toBeTruthy();
    expect(close).not.toHaveBeenCalled();
    expect(
      Number(
        (screen.getByRole('spinbutton', { name: 'Download limit' }) as HTMLInputElement).value,
      ),
    ).toBe(20);
    await waitFor(() =>
      expect((screen.getByRole('button', { name: /Save/ }) as HTMLButtonElement).disabled).toBe(
        false,
      ),
    );
    submit();
    await waitFor(() => expect(close).toHaveBeenCalledWith(false));
    expect(save.mock.calls[1][1]).toEqual(expect.objectContaining({ email: 'renamed@test' }));
    expect(
      post.mock.calls
        .filter(([url]) => url.endsWith('/bandwidth'))
        .every(([url]) => url === '/panel/api/clients/renamed%40test/bandwidth'),
    ).toBe(true);
  });
  it('does not restart bandwidth configuration for an unchanged limit', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', defaults));
    const post = vi.spyOn(HttpUtil, 'post');
    const { close } = mount();
    await screen.findByRole('spinbutton', { name: 'Download limit' });
    post.mockClear();
    submit();
    await waitFor(() => expect(close).toHaveBeenCalledWith(false));
    expect(post).not.toHaveBeenCalled();
  });
  it('allows clearing both limits to unlimited', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(true, '', defaults));
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(true, ''));
    mount();
    await changeDownload('0');
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Upload limit' }), {
      target: { value: '0' },
    });
    submit();
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        expect.any(String),
        { uploadMbps: 0, downloadMbps: 0 },
        { silent: true, headers: { 'Content-Type': 'application/json' } },
      ),
    );
  });
  it('blocks Save on load failure and allows retry without overwriting unknown limits', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockResolvedValue(new Msg(false, 'offline'));
    const { save } = mount();
    await screen.findByText('Could not load bandwidth settings. Refresh them before saving.');
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true);
    submit();
    expect(save).not.toHaveBeenCalled();
    get.mockResolvedValue(new Msg(true, '', defaults));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await screen.findByRole('spinbutton', { name: 'Upload limit' });
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false);
  });
  it('blocks a changed nonzero limit on an unsupported core', async () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue(
      new Msg(true, '', { ...defaults, uploadMbps: 0, downloadMbps: 0, supported: false }),
    );
    const { save } = mount();
    await changeDownload('20');
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true);
    submit();
    expect(save).not.toHaveBeenCalled();
  });
});
