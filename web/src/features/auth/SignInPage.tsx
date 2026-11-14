import { useQuery } from '@tanstack/react-query';
import { useSearchParams } from 'react-router';
import { useApi } from '../../api/context';
import { ErrorText } from '../../components/ErrorText';
import { signinErrorMessage } from './signinErrors';

const PROVIDER_NAMES: ReadonlyMap<string, string> = new Map([
  ['google', 'Google'],
  ['github', 'GitHub'],
]);

/** The page signed-out visitors see: one button per configured provider. */
export function SignInPage() {
  const api = useApi();
  const [params] = useSearchParams();
  const providers = useQuery({
    queryKey: ['auth', 'providers'],
    queryFn: () => api.get<{ providers: string[] }>('/auth/providers'),
  });
  const error = signinErrorMessage(params.get('signin_error'));

  return (
    <main className="signin">
      <h1>Kubelook</h1>
      <p>Sign in to continue.</p>
      <ErrorText message={error} />
      {providers.isError && <p>The sign-in options could not be loaded.</p>}
      {providers.data?.providers.length === 0 && <p>No sign-in provider is configured.</p>}
      <ul>
        {providers.data?.providers.map((p) => (
          <li key={p}>
            <a href={`/auth/${encodeURIComponent(p)}/login`}>Sign in with {PROVIDER_NAMES.get(p) ?? p}</a>
          </li>
        ))}
      </ul>
    </main>
  );
}
