import { useQuery } from '@tanstack/react-query';
import { useApi } from '../../api/context';

export interface Certificate {
  key: string;
  index: number;
  subject: string;
  issuer: string;
  serial: string;
  notBefore: string;
  notAfter: string;
  dnsNames: string[];
  isCA: boolean;
  expiresInSeconds: number;
  error?: string;
}

const WARN_SECONDS = 30 * 24 * 3600;

/** Whether the key names suggest certificates; only then the server is asked (each look is audited). */
export function mayHoldCertificates(secret: Record<string, unknown>): boolean {
  const type = typeof secret.type === 'string' ? secret.type : '';
  const keys = Object.keys((secret.data as Record<string, unknown> | undefined) ?? {});
  return type === 'kubernetes.io/tls' || keys.some((k) => /\.(crt|pem|cert|cer)$/i.test(k));
}

function expiry(c: Certificate): string {
  const days = Math.floor(Math.abs(c.expiresInSeconds) / 86400);
  if (c.expiresInSeconds < 0) return days > 0 ? `expired ${days} days ago` : 'expired today';
  return days > 0 ? `in ${days} days` : 'in less than a day';
}

/** Metadata of the PEM certificates in a Secret: subject, issuer, validity, names. Expiry under 30 days is flagged. */
export function CertificateDetails({
  cluster,
  namespace,
  name,
  enabled,
}: {
  cluster: string;
  namespace: string;
  name: string;
  enabled: boolean;
}) {
  const api = useApi();
  const q = useQuery({
    queryKey: ['certificates', cluster, namespace, name],
    enabled,
    staleTime: 60_000,
    retry: false,
    queryFn: () =>
      api.post<{ certificates: Certificate[] }>(
        `/clusters/${encodeURIComponent(cluster)}/secrets/certificates`,
        { namespace, name },
      ),
  });
  const certs = q.data?.certificates ?? [];
  if (certs.length === 0) return null;
  return (
    <section aria-label="Certificates">
      <h3>Certificates</h3>
      <table aria-label="Certificate list">
        <thead>
          <tr>
            <th>Key</th>
            <th>Subject</th>
            <th>Issuer</th>
            <th>Names</th>
            <th>Expires</th>
          </tr>
        </thead>
        <tbody>
          {certs.map((c) => (
            <tr
              key={`${c.key}-${c.index}`}
              className={
                c.error ? 'cert-error' : c.expiresInSeconds < WARN_SECONDS ? 'cert-warning' : undefined
              }
            >
              <td>{`${c.key}${c.index > 0 ? ` #${c.index + 1}` : ''}`}</td>
              {c.error ? (
                <td colSpan={4}>{c.error}</td>
              ) : (
                <>
                  <td>{c.subject}</td>
                  <td>{c.issuer}</td>
                  <td>{c.dnsNames.join(', ')}</td>
                  <td>
                    {c.expiresInSeconds < WARN_SECONDS && (
                      <span role="img" aria-label="expires soon">
                        !{' '}
                      </span>
                    )}
                    {expiry(c)}
                  </td>
                </>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
