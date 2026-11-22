import { useQueryClient } from '@tanstack/react-query';
import { useApi } from '../../api/context';
import { Button } from '../../components/Button';

/** Ends the session on the server, then asks for who is signed in again, which shows the sign-in page. */
export function SignOutButton() {
  const api = useApi();
  const queryClient = useQueryClient();
  async function signOut() {
    try {
      await api.post('/auth/logout');
    } finally {
      await queryClient.invalidateQueries({ queryKey: ['me'] });
    }
  }
  return <Button onClick={() => void signOut()}>Sign out</Button>;
}
