import { Link } from 'react-router';
import { Banner } from '../../components/Banner';
import { getString } from '../../lib/path';
import { resourcePath } from '../../lib/paths';
import type { K8sObject } from '../../state/live';

/** Warns on a Secret the Vault Secrets Operator made: edits are overwritten, and the VaultSecret is the thing to change. */
export function SecretOperatorBanner({ cluster, secret }: { cluster: string; secret: K8sObject }) {
  const labels = (secret.metadata as { labels?: Record<string, string> } | undefined)?.labels ?? {};
  if (labels['created-by'] !== 'vault-secrets-operator') return null;
  const name = getString(secret, 'metadata.name');
  return (
    <Banner kind="warning">
      <span>Edits will be overwritten by the operator. </span>
      <Link
        to={resourcePath({
          cluster,
          namespace: getString(secret, 'metadata.namespace'),
          group: 'ricoberger.de',
          kind: 'VaultSecret',
          name,
        })}
      >{`VaultSecret ${name}`}</Link>
    </Banner>
  );
}
