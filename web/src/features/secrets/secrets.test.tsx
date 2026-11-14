import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { bytesToBase64, encodeUtf8 } from '../../lib/bytes';
import { jsonResponse, renderApp } from '../../test/render';
import { CertificateDetails, mayHoldCertificates } from './CertificateDetails';
import { SecretData } from './SecretData';

const secret = {
  metadata: { name: 'db', namespace: 'ns' },
  type: 'Opaque',
  data: { password: '********', blob: '********' },
};

function mount(
  answer: (body: { keys?: string[] }) => Response = () => jsonResponse({ data: {} }),
  props: Partial<React.ComponentProps<typeof SecretData>> = {},
) {
  const requests: { url: string; body: { namespace: string; name: string; keys?: string[] } }[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body)) as { namespace: string; name: string; keys?: string[] };
    requests.push({ url, body });
    return Promise.resolve(answer(body));
  });
  const view = renderApp(<SecretData cluster="v:prod" secret={secret} {...props} />, {
    client: createClient({ fetch }),
  });
  return { requests, ...view };
}

const b64 = (s: string) => bytesToBase64(encodeUtf8(s));

describe('SecretData', () => {
  it('hides every value until it is revealed, and lists the keys sorted', () => {
    mount();
    const items = screen.getAllByRole('listitem');
    expect(items.map((i) => i.textContent?.split(':')[0])).toEqual(['blob', 'password']);
    expect(screen.getAllByText('••••••••')).toHaveLength(2);
    expect(screen.queryByText('********')).toBeNull();
  });

  it('reveals one key by asking the server for that key and shows it as text with a copy button', async () => {
    const clipboard = { writeText: vi.fn(() => Promise.resolve()) };
    const { requests } = mount((b) => jsonResponse({ data: { [b.keys?.[0] ?? '']: b64('s3cr3t <b>') } }), {
      clipboard,
    });
    await userEvent.click(screen.getByRole('button', { name: 'Reveal password' }));
    expect(await screen.findByText('s3cr3t <b>')).toBeInTheDocument();
    expect(requests[0]).toEqual({
      url: '/api/v1/clusters/v%3Aprod/secrets/reveal',
      body: { namespace: 'ns', name: 'db', keys: ['password'] },
    });
    expect(screen.getByText('blob').closest('li')).toHaveTextContent('••••••••');
    await userEvent.click(screen.getByRole('button', { name: 'Copy password' }));
    expect(clipboard.writeText).toHaveBeenCalledWith('s3cr3t <b>');
    await userEvent.click(screen.getByRole('button', { name: 'Hide password' }));
    expect(screen.queryByText('s3cr3t <b>')).toBeNull();
  });

  it('reveal all and hide all', async () => {
    const { requests } = mount(() => jsonResponse({ data: { password: b64('a'), blob: b64('b') } }));
    await userEvent.click(screen.getByRole('button', { name: 'Reveal all' }));
    expect(await screen.findByRole('button', { name: 'Hide all' })).toBeInTheDocument();
    expect(requests[0]?.body).toEqual({ namespace: 'ns', name: 'db' });
    await userEvent.click(screen.getByRole('button', { name: 'Hide all' }));
    expect(screen.getAllByText('••••••••')).toHaveLength(2);
  });

  it('shows binary values as a size with a download button', async () => {
    const saveFile = vi.fn();
    const binary = bytesToBase64(new Uint8Array([0xff, 0xfe, 0x00, 0x01]));
    mount(() => jsonResponse({ data: { blob: binary } }), { saveFile });
    await userEvent.click(screen.getByRole('button', { name: 'Reveal blob' }));
    expect(await screen.findByText('binary, 4 B (4 bytes)')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Download blob' }));
    expect(saveFile).toHaveBeenCalledWith('blob', new Uint8Array([0xff, 0xfe, 0x00, 0x01]));
  });

  it('shows a refusal as text and keeps the value hidden', async () => {
    mount(() => jsonResponse({ error: { code: 'forbidden', message: 'secrets "db" is forbidden' } }, 403));
    await userEvent.click(screen.getByRole('button', { name: 'Reveal password' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('secrets "db" is forbidden');
    expect(screen.getAllByText('••••••••')).toHaveLength(2);
  });

  it('values are gone once the component unmounts', async () => {
    const { unmount } = mount(() => jsonResponse({ data: { password: b64('gone') } }));
    await userEvent.click(screen.getByRole('button', { name: 'Reveal password' }));
    await screen.findByText('gone');
    unmount();
    renderApp(<SecretData cluster="v:prod" secret={secret} />);
    expect(screen.queryByText('gone')).toBeNull();
  });

  it('says so for a Secret without keys', () => {
    renderApp(<SecretData cluster="c" secret={{ metadata: { name: 'x', namespace: 'n' } }} />);
    expect(screen.getByText('This Secret has no keys.')).toBeInTheDocument();
  });
});

describe('CertificateDetails', () => {
  const cert = (over: Record<string, unknown> = {}) => ({
    key: 'tls.crt',
    index: 0,
    subject: 'CN=web.example',
    issuer: 'CN=Root',
    serial: '1',
    notBefore: '2026-01-01T00:00:00Z',
    notAfter: '2027-01-01T00:00:00Z',
    dnsNames: ['web.example'],
    isCA: false,
    expiresInSeconds: 90 * 86400,
    ...over,
  });

  function mount(certificates: unknown[], enabled = true) {
    const fetch = vi.fn(() => Promise.resolve(jsonResponse({ certificates })));
    renderApp(<CertificateDetails cluster="v:prod" namespace="ns" name="tls" enabled={enabled} />, {
      client: createClient({ fetch }),
    });
    return fetch;
  }

  it('lists subject, issuer, names and the time to expiry; no warning for a long validity', async () => {
    mount([cert()]);
    const table = await screen.findByRole('table', { name: 'Certificate list' });
    expect(within(table).getByText('CN=web.example')).toBeInTheDocument();
    expect(within(table).getByText('web.example')).toBeInTheDocument();
    expect(within(table).getByText('in 90 days')).toBeInTheDocument();
    expect(within(table).queryByRole('img', { name: 'expires soon' })).toBeNull();
  });

  it('warns under 30 days, and says when a certificate has expired or expires today', async () => {
    mount([
      cert({ expiresInSeconds: 10 * 86400 }),
      cert({ key: 'old.crt', expiresInSeconds: -3 * 86400 }),
      cert({ key: 'now.crt', index: 1, expiresInSeconds: 3600 }),
      cert({ key: 'gone.crt', expiresInSeconds: -60 }),
    ]);
    const table = await screen.findByRole('table', { name: 'Certificate list' });
    expect(within(table).getByText('in 10 days')).toBeInTheDocument();
    expect(within(table).getByText('expired 3 days ago')).toBeInTheDocument();
    expect(within(table).getByText('in less than a day')).toBeInTheDocument();
    expect(within(table).getByText('expired today')).toBeInTheDocument();
    expect(within(table).getByText('now.crt #2')).toBeInTheDocument();
    expect(within(table).getAllByRole('img', { name: 'expires soon' })).toHaveLength(4);
  });

  it('shows the error of a certificate that could not be read', async () => {
    mount([cert({ error: 'The certificate could not be read: bad' })]);
    expect(await screen.findByText('The certificate could not be read: bad')).toBeInTheDocument();
  });

  it('asks nothing when disabled and shows nothing without certificates', async () => {
    const f = mount([], false);
    expect(f).not.toHaveBeenCalled();
    const g = mount([]);
    await vi.waitFor(() => expect(g).toHaveBeenCalled());
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('only secrets that look like they hold certificates are looked at', () => {
    expect(mayHoldCertificates({ type: 'kubernetes.io/tls', data: {} })).toBe(true);
    expect(mayHoldCertificates({ type: 'Opaque', data: { 'ca.crt': 'x' } })).toBe(true);
    expect(mayHoldCertificates({ type: 'Opaque', data: { 'bundle.PEM': 'x' } })).toBe(true);
    expect(mayHoldCertificates({ type: 'Opaque', data: { password: 'x' } })).toBe(false);
    expect(mayHoldCertificates({})).toBe(false);
  });
});
