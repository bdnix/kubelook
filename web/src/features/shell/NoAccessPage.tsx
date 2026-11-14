export type NoAccessReason = 'unknown' | 'forbidden';

const MESSAGES: Record<NoAccessReason, { title: string; text: string }> = {
  unknown: {
    title: 'Cluster not available',
    text: "This cluster isn't available in your session. If it comes from a kubeconfig, upload that kubeconfig first.",
  },
  forbidden: { title: 'No access', text: "You don't have access to this cluster." },
};

/** What a deep link to a cluster shows when the cluster cannot be used. */
export function NoAccessPage({ reason }: { reason: NoAccessReason }) {
  const { title, text } = MESSAGES[reason];
  return (
    <>
      <h1>{title}</h1>
      <p>{text}</p>
    </>
  );
}
