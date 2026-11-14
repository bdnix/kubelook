import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { vaultStatusStore, type VaultStatus } from '../../state/vaultStatus';
import type { Source } from '../clusters/useClusters';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { Banners } from './Banners';

const NOW = '2026-11-14T12:00:00Z';
const base: VaultStatus = {
  displayName: 'ada',
  entityId: 'e',
  expiresAt: '2026-11-14T12:20:00Z',
  ttlSeconds: 1200,
  renewable: true,
  canRenew: true,
  neverExpires: false,
  warning: false,
  expired: false,
};
const none: Source[] = [];

function renderBanners(
  props: Partial<Parameters<typeof Banners>[0]> = {},
  extra: Parameters<typeof renderApp>[1] = {},
) {
  return renderApp(
    <Banners maintenanceBanner="" sources={none} vaultWarningSeconds={1800} notices={[]} {...props} />,
    extra,
  );
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.setSystemTime(new Date(NOW));
  vaultStatusStore.getState().clear();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('Banners', () => {
  it('shows nothing when there is nothing to say', () => {
    renderBanners();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('shows the maintenance banner', () => {
    renderBanners({ maintenanceBanner: 'Back at noon' });
    expect(screen.getByRole('status')).toHaveTextContent('Back at noon');
  });

  it('shows the notices it is given', () => {
    renderBanners({ notices: ['First notice', 'Second notice'] });
    expect(screen.getByText('First notice')).toBeInTheDocument();
    expect(screen.getByText('Second notice')).toBeInTheDocument();
  });

  it('warns with the remaining time when the expiry is within the warning window, and counts down each minute', () => {
    vaultStatusStore.getState().set(base);
    renderBanners();
    expect(screen.getByRole('alert')).toHaveTextContent('Your Vault token expires in 20 minutes.');
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(screen.getByRole('alert')).toHaveTextContent('expires in 19 minutes');
  });

  it('does not warn while the expiry is outside the warning window, then starts to', () => {
    vaultStatusStore.getState().set({ ...base, expiresAt: '2026-11-14T13:00:00Z' });
    renderBanners();
    expect(screen.queryByRole('alert')).toBeNull();
    act(() => {
      vi.advanceTimersByTime(31 * 60_000);
    });
    expect(screen.getByRole('alert')).toHaveTextContent('expires in 29 minutes');
  });

  it('never warns for a token that never expires', () => {
    vaultStatusStore.getState().set({ ...base, neverExpires: true, expiresAt: null, canRenew: false });
    renderBanners();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('offers Renew when the token can be renewed, and shows the new status', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    vaultStatusStore.getState().set(base);
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries');
    const client = fakeClient({
      'POST /credentials/vault/renew': () =>
        jsonResponse({ vault: { ...base, expiresAt: '2026-11-14T12:50:00Z', ttlSeconds: 3000 } }),
    });
    renderBanners({}, { client, queryClient });
    await user.click(screen.getByRole('button', { name: 'Renew' }));
    await waitFor(() => expect(vaultStatusStore.getState().status?.ttlSeconds).toBe(3000));
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['clusters'] });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows why renewing failed', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    vaultStatusStore.getState().set(base);
    const client = fakeClient({
      'POST /credentials/vault/renew': () =>
        jsonResponse({ error: { code: 'conflict', message: 'This Vault token cannot be renewed.' } }, 409),
    });
    renderBanners({}, { client });
    await user.click(screen.getByRole('button', { name: 'Renew' }));
    expect(await screen.findByText('This Vault token cannot be renewed.')).toBeInTheDocument();
  });

  it('without canRenew tells the user to get a new token and paste it in settings', () => {
    vaultStatusStore.getState().set({ ...base, canRenew: false });
    renderBanners();
    expect(screen.queryByRole('button', { name: 'Renew' })).toBeNull();
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Get a new token from Vault and paste it in settings.',
    );
  });

  it('says the token has expired once the time has passed, whatever the last status said', () => {
    vaultStatusStore.getState().set({ ...base, expiresAt: '2026-11-14T12:01:00Z', canRenew: true });
    renderBanners();
    act(() => {
      vi.advanceTimersByTime(2 * 60_000);
    });
    expect(screen.getByRole('alert')).toHaveTextContent('Your Vault token has expired.');
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Get a new token from Vault and paste it in settings.',
    );
    expect(screen.queryByRole('button', { name: 'Renew' })).toBeNull();
  });

  it('says the token has expired when the server says so', () => {
    vaultStatusStore.getState().set({ ...base, expired: true, expiresAt: null, ttlSeconds: 0 });
    renderBanners();
    expect(screen.getByRole('alert')).toHaveTextContent('Your Vault token has expired.');
  });

  it('shows a banner when the Vault source is unreachable', () => {
    const sources: Source[] = [{ type: 'vault', label: 'Vault', status: 'unreachable', clusters: [] }];
    renderBanners({ sources });
    expect(screen.getByRole('alert')).toHaveTextContent(
      "Vault is unreachable. Open clusters keep working until their credentials expire; new Vault clusters can't be opened.",
    );
  });

  it('shows the Vault banner while the server reports Vault unreachable, and not otherwise', () => {
    const { rerender } = renderBanners({ vaultUnreachable: true });
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Open clusters keep working until their credentials expire',
    );
    rerender(
      <Banners
        maintenanceBanner=""
        sources={none}
        vaultWarningSeconds={1800}
        notices={[]}
        vaultUnreachable={false}
      />,
    );
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('shows no unreachable banner for kubeconfig sources', () => {
    const sources: Source[] = [{ type: 'kubeconfig', label: 'p', status: 'unreachable', clusters: [] }];
    renderBanners({ sources });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
