import { VaultTokenForm } from '../credentials/VaultTokenForm';

/**
 * Replaces the Vault token of the session. Terminals, log streams and editors keep running: the server keeps the
 * credentials it already minted for the clusters and mints new ones with the new token when they are renewed.
 */
export function NewTokenBox() {
  return (
    <section aria-label="Replace the Vault token">
      <h2>Replace the Vault token</h2>
      <p>Open terminals, logs and editors are not closed.</p>
      <VaultTokenForm
        label="Paste a new Vault token"
        submitLabel="Replace Vault token"
        formLabel="Replace the Vault token"
      />
    </section>
  );
}
