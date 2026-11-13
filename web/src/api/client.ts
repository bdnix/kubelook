// The only module that calls fetch (enforced by lint). Everything else receives an ApiClient.

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

export interface ClientDeps {
  fetch: FetchFn;
  /** The CSRF token of the current session, if any. */
  getCsrf?: () => string | undefined;
}

export interface ApiClient {
  get<T = unknown>(path: string): Promise<T>;
  post<T = unknown>(path: string, body?: unknown): Promise<T>;
  put<T = unknown>(path: string, body?: unknown): Promise<T>;
  patch<T = unknown>(path: string, body?: unknown): Promise<T>;
  delete<T = unknown>(path: string, body?: unknown): Promise<T>;
}

const BASE = '/api/v1';

interface ErrorBody {
  error?: { code?: unknown; message?: unknown };
}

async function parseError(res: Response): Promise<ApiError> {
  let body: ErrorBody | undefined;
  try {
    body = (await res.json()) as ErrorBody;
  } catch {
    body = undefined;
  }
  const { code, message } = body?.error ?? {};
  if (typeof code === 'string' && typeof message === 'string') {
    return new ApiError(res.status, code, message);
  }
  return new ApiError(res.status, 'internal', `Unexpected response (HTTP ${res.status})`);
}

export function createClient({ fetch, getCsrf }: ClientDeps): ApiClient {
  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers = new Headers();
    if (method !== 'GET') {
      const csrf = getCsrf?.();
      if (csrf) headers.set('X-CSRF-Token', csrf);
    }
    const init: RequestInit = { method, credentials: 'same-origin', headers };
    if (body !== undefined) {
      headers.set('Content-Type', 'application/json');
      init.body = JSON.stringify(body);
    }

    let res: Response;
    try {
      res = await fetch(BASE + path, init);
    } catch {
      throw new ApiError(0, 'network', 'The server could not be reached.');
    }
    if (!res.ok) throw await parseError(res);

    const text = await res.text();
    if (res.status === 204 || text === '') return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      throw new ApiError(res.status, 'internal', 'The server sent a response that is not JSON.');
    }
  }

  return {
    get: (path) => request('GET', path),
    post: (path, body) => request('POST', path, body),
    put: (path, body) => request('PUT', path, body),
    patch: (path, body) => request('PATCH', path, body),
    delete: (path, body) => request('DELETE', path, body),
  };
}

/** The client the running app uses: the browser's fetch. */
export function browserClient(getCsrf?: () => string | undefined): ApiClient {
  return createClient({ fetch: (input, init) => fetch(input, init), getCsrf });
}
