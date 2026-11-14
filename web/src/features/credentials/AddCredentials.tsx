import { KubeconfigForm } from './KubeconfigForm';
import { VaultTokenForm } from './VaultTokenForm';

export interface AddCredentialsProps {
  features: { vault: boolean; kubeconfig: boolean };
}

/** The forms for adding credentials; each is shown only when the server offers it. */
export function AddCredentials({ features }: AddCredentialsProps) {
  if (!features.vault && !features.kubeconfig) {
    return <p>This server accepts no credentials. Ask an administrator.</p>;
  }
  return (
    <section aria-label="Add credentials">
      {features.vault && <VaultTokenForm />}
      {features.kubeconfig && <KubeconfigForm />}
    </section>
  );
}
