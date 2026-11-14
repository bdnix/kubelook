import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useApi } from '../../api/context';
import { Banner } from '../../components/Banner';
import { useNow } from '../../lib/clock';
import { useVaultStatus, type VaultStatus } from '../../state/vaultStatus';
import { errorText } from '../credentials/errors';
import type { Source } from '../clusters/useClusters';

const MINUTE = 60_000;
const NEW_TOKEN = 'Get a new token from Vault and paste it in settings.';

export interface BannersProps {
  maintenanceBanner: string;
  sources: Source[] | undefined;
  /** How long before expiry the Vault warning starts (from `me.limits`). */
  vaultWarningSeconds: number;
  /** One-off messages, such as what restoring the saved credentials found. */
  notices: string[];
}

/** Everything the app wants the user to know at the top of the page. */
export function Banners({ maintenanceBanner, sources, vaultWarningSeconds, notices }: BannersProps) {
  const unreachable = sources?.some((s) => s.type === 'vault' && s.status === 'unreachable') ?? false;
  return (
    <div className="banners">
      {maintenanceBanner !== '' && <Banner kind="info">{maintenanceBanner}</Banner>}
      {notices.map((n) => (
        <Banner key={n} kind="warning">
          {n}
        </Banner>
      ))}
      {unreachable && <Banner kind="error">Vault is unreachable.</Banner>}
      <VaultExpiry warningSeconds={vaultWarningSeconds} />
    </div>
  );
}

function VaultExpiry({ warningSeconds }: { warningSeconds: number }) {
  const api = useApi();
  const queryClient = useQueryClient();
  const { status, set } = useVaultStatus();
  const now = useNow(MINUTE);
  const [renewError, setRenewError] = useState<string>();

  if (!status || status.neverExpires) return null;
  const remaining = status.expiresAt === null ? 0 : Date.parse(status.expiresAt) - now;
  if (status.expired || remaining <= 0) {
    return <Banner kind="error">{`Your Vault token has expired. ${NEW_TOKEN}`}</Banner>;
  }
  if (remaining >= warningSeconds * 1000) return null;

  const minutes = Math.ceil(remaining / MINUTE);
  const text = `Your Vault token expires in ${minutes} ${minutes === 1 ? 'minute' : 'minutes'}.`;

  async function renew() {
    setRenewError(undefined);
    try {
      const { vault } = await api.post<{ vault: VaultStatus }>('/credentials/vault/renew');
      set(vault);
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
    } catch (err) {
      setRenewError(errorText(err));
    }
  }

  return (
    <Banner kind="warning" action={status.canRenew ? { label: 'Renew', onClick: () => void renew() } : undefined}>
      {status.canRenew ? text : `${text} ${NEW_TOKEN}`}
      {renewError && <span> {renewError}</span>}
    </Banner>
  );
}
