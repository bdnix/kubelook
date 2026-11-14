import { Link } from 'react-router';
import { groupOf, type ChainLink } from '../../lib/owners';
import { resourcePath } from '../../lib/paths';

/** Pod → ReplicaSet → Deployment, each a link to the owner's details. Renders nothing without owners. */
export function OwnerChain({
  chain,
  cluster,
  namespace,
}: {
  chain: ChainLink[];
  cluster: string;
  namespace: string;
}) {
  if (chain.length === 0) return null;
  return (
    <nav aria-label="Owner chain">
      <ol>
        {chain.map(({ ref, object }) => (
          <li key={ref.uid || `${ref.kind}/${ref.name}`}>
            <Link
              to={resourcePath({
                cluster,
                namespace,
                group: groupOf(ref.apiVersion),
                kind: ref.kind,
                name: ref.name,
              })}
            >
              {`${ref.kind}/${ref.name}`}
            </Link>
            {object === undefined && <span> (not found)</span>}
          </li>
        ))}
      </ol>
    </nav>
  );
}
