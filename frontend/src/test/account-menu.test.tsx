import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import AccountMenu from '@/layouts/AccountMenu';
import { HttpUtil, Msg } from '@/utils';

afterEach(() => {
  document.querySelector('meta[name="current-user"]')?.remove();
  vi.restoreAllMocks();
});

describe('account menu', () => {
  it('shows the authenticated account and lets a failed logout be retried', async () => {
    const meta = document.createElement('meta');
    meta.name = 'current-user';
    meta.content = 'Admin';
    document.head.append(meta);
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue(new Msg(false, 'Offline'));

    render(<AccountMenu />);
    expect(screen.getByText('Admin')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Administrator account' }));
    fireEvent.click(await screen.findByText('Log Out'));

    await waitFor(() => expect(post).toHaveBeenCalledWith('/logout'));
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Administrator account' }).hasAttribute('disabled'),
      ).toBe(false),
    );
  });
});
