import { useId, useState } from 'react';
import { Link } from 'react-router';
import { clusterPath } from '../../lib/paths';
import { VAULT_DOWN } from '../shell/Banners';
import type { ClusterEntry, ClusterState, Source } from './useClusters';

const STATE_LABELS: Record<ClusterState, string> = {
  open: 'connected',
  disconnected: 'disconnected',
  closed: 'not opened',
};

function Group({ title, children }: { title: string; children: React.ReactNode }) {
  const id = useId();
  return (
    <section role="group" aria-labelledby={id} className="rail-group">
      <h2 id={id}>{title}</h2>
      <ul>{children}</ul>
    </section>
  );
}

/** The cluster rail: favourites first, then one group per credential source. */
export function ClusterRail({
  sources,
  favourites,
  vaultUnreachable = false,
}: {
  sources: Source[];
  favourites: string[];
  /** Closed Vault clusters cannot be opened while Vault is down. */
  vaultUnreachable?: boolean;
}) {
  const [roles, setRoles] = useState<Record<string, string>>({});
  const favouriteSet = new Set(favourites);

  const row = (c: ClusterEntry, vault: boolean) => (
    <li key={c.id}>
      <span role="img" aria-label={STATE_LABELS[c.state]} className={`dot dot-${c.state}`} />
      {vault && vaultUnreachable && c.state === 'closed' ? (
        <button type="button" disabled title={VAULT_DOWN} aria-label={`Open ${c.name}. ${VAULT_DOWN}`}>
          {c.name}
        </button>
      ) : (
        <Link to={clusterPath(c.id)}>{c.name}</Link>
      )}
      {vault && c.roles && c.roles.length > 1 && (
        <select
          aria-label={`Role for ${c.name}`}
          value={roles[c.id] ?? c.selectedRole ?? c.roles[0]}
          onChange={(e) => setRoles((r) => ({ ...r, [c.id]: e.target.value }))}
        >
          {c.roles.map((role) => (
            <option key={role} value={role}>
              {role}
            </option>
          ))}
        </select>
      )}
    </li>
  );

  const pinned = sources.flatMap((s) =>
    s.clusters.filter((c) => favouriteSet.has(c.id)).map((c) => row(c, s.type === 'vault')),
  );
  if (sources.length === 0) return <p>No clusters yet.</p>;

  return (
    <>
      {pinned.length > 0 && <Group title="Favourites">{pinned}</Group>}
      {sources.map((s) => (
        <Group key={`${s.type}:${s.label}`} title={s.label}>
          {s.clusters.filter((c) => !favouriteSet.has(c.id)).map((c) => row(c, s.type === 'vault'))}
        </Group>
      ))}
    </>
  );
}
