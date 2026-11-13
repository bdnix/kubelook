import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, browserClient, createClient } from './client';

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function clientWith(response: Response | Error, csrf: string | null = 'tok') {
  const fetch = vi.fn<(input: string, init?: RequestInit) => Promise<Response>>(() =>
    response instanceof Error ? Promise.reject(response) : Promise.resolve(response),
  );
  return { fetch, client: createClient({ fetch, getCsrf: () => csrf ?? undefined }) };
}

describe('createClient', () => {
  it('get calls /api/v1 + path with same-origin credentials and returns the parsed body', async () => {
    const { fetch, client } = clientWith(json({ a: 1 }));
    await expect(client.get('/x')).resolves.toEqual({ a: 1 });
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/x',
      expect.objectContaining({ method: 'GET', credentials: 'same-origin' }),
    );
  });

  it('keeps the query string', async () => {
    const { fetch, client } = clientWith(json({}));
    await client.get('/x?y=1&z=2');
    expect(fetch).toHaveBeenCalledWith('/api/v1/x?y=1&z=2', expect.anything());
  });

  it('get does not send a CSRF token or a content type', async () => {
    const { fetch, client } = clientWith(json({}));
    await client.get('/x');
    const init = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false);
    expect(init.body).toBeUndefined();
  });

  it('post sends JSON, content type and the CSRF token', async () => {
    const { fetch, client } = clientWith(json({ ok: true }));
    await client.post('/x', { name: 'n' });
    const init = fetch.mock.calls[0]?.[1] as RequestInit;
    const headers = new Headers(init.headers);
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(headers.get('Content-Type')).toBe('application/json');
    expect(headers.get('X-CSRF-Token')).toBe('tok');
    expect(init.body).toBe('{"name":"n"}');
  });

  it.each([
    ['put', 'PUT'],
    ['patch', 'PATCH'],
    ['delete', 'DELETE'],
  ] as const)('%s uses %s and sends the CSRF token', async (method, verb) => {
    const { fetch, client } = clientWith(json({}));
    await client[method]('/x', { a: 1 });
    const init = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe(verb);
    expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('tok');
  });

  it('post without a token or a body sends neither', async () => {
    const { fetch, client } = clientWith(json({}), null);
    await client.post('/x');
    const init = fetch.mock.calls[0]?.[1] as RequestInit;
    expect(new Headers(init.headers).has('X-CSRF-Token')).toBe(false);
    expect(init.body).toBeUndefined();
  });

  it('turns a JSON error body into ApiError', async () => {
    const { client } = clientWith(json({ error: { code: 'conflict', message: 'Changed meanwhile.' } }, 409));
    const err = await client.get('/x').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 409, code: 'conflict', message: 'Changed meanwhile.' });
  });

  it('turns a non-JSON error body into ApiError with code internal', async () => {
    const { client } = clientWith(new Response('<html>Bad gateway</html>', { status: 502 }));
    await expect(client.get('/x')).rejects.toMatchObject({ status: 502, code: 'internal' });
  });

  it('turns JSON of the wrong shape into ApiError with code internal', async () => {
    const { client } = clientWith(json({ nope: true }, 500));
    await expect(client.get('/x')).rejects.toMatchObject({ status: 500, code: 'internal' });
  });

  it('returns undefined for 204 and for an empty 200', async () => {
    await expect(clientWith(new Response(null, { status: 204 })).client.get('/x')).resolves.toBeUndefined();
    await expect(clientWith(new Response('', { status: 200 })).client.get('/x')).resolves.toBeUndefined();
  });

  it('rejects a successful response that is not JSON', async () => {
    const { client } = clientWith(new Response('<html></html>', { status: 200 }));
    await expect(client.get('/x')).rejects.toMatchObject({ status: 200, code: 'internal' });
  });

  it('turns a network failure into ApiError status 0', async () => {
    const { client } = clientWith(new TypeError('failed to fetch'));
    await expect(client.get('/x')).rejects.toMatchObject({ status: 0, code: 'network' });
  });
});

describe('browserClient', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('uses the global fetch', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(json({ hi: 1 })));
    vi.stubGlobal('fetch', fetchMock);
    await expect(browserClient(() => 'tok').get('/hello')).resolves.toEqual({ hi: 1 });
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/hello', expect.anything());
  });
});
