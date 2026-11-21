import { NavLink } from 'react-router';
import type { DiscoveryResource } from '../../api/discovery';
import { clusterPath } from '../../lib/paths';

/** The kinds of the Custom Resources section, under one heading per API group (the kinds arrive sorted). */
export function CrdSidebarSection({ cluster, kinds }: { cluster: string; kinds: DiscoveryResource[] }) {
  const groups = [...new Set(kinds.map((k) => k.group))];
  return (
    <>
      {groups.map((group) => (
        <div key={group} role="group" aria-label={group}>
          <h3>{group}</h3>
          <ul>
            {kinds
              .filter((k) => k.group === group)
              .map((k) => (
                <li key={k.kind}>
                  <NavLink to={`${clusterPath(cluster)}/${group}/${k.kind}`} end>
                    {k.kind}
                  </NavLink>
                </li>
              ))}
          </ul>
        </div>
      ))}
    </>
  );
}
