import type { ApiClient } from '../../api/client';
import type { VaultStore } from './vaultStore';

interface PendingCopy {
  label: string;
  sealed: string;
}

/**
 * Takes the kubeconfig copies the server re-sealed (after an OIDC refresh token rotated) into this browser, replacing the
 * saved copy of each label, and only then tells the server it may forget them. A failure leaves everything as it was:
 * the server keeps the copies and the next call tries again.
 */
export async function syncPending(api: ApiClient, store: VaultStore): Promise<void> {
  try {
    const { items } = await api.get<{ items: PendingCopy[] }>('/credentials/pending');
    if (items.length === 0) return;
    const savedAt = new Date().toISOString();
    for (const { label, sealed } of items) {
      await store.put({ id: `kubeconfig:${label}`, kind: 'kubeconfig', label, sealed, savedAt });
    }
    await api.post('/credentials/pending/ack', { labels: items.map((i) => i.label) });
  } catch {
    // Nothing is lost: the pending copies stay on the server until they are acknowledged.
  }
}
