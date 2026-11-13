import { Route, Routes, useParams } from 'react-router';
import { useMe } from '../../api/me';
import { Banner } from '../../components/Banner';
import { Layout } from './Layout';

function SignInPlaceholder() {
  return (
    <main>
      <h1>Kubelook</h1>
      <p>Sign in to continue.</p>
    </main>
  );
}

function ClusterPicker() {
  return <h1>Choose a cluster</h1>;
}

function SettingsPage() {
  return <h1>Settings</h1>;
}

function ClusterPage() {
  const { cluster } = useParams();
  return (
    <>
      <h1>Cluster</h1>
      <p>{cluster}</p>
    </>
  );
}

function NotFound() {
  return <p>Page not found</p>;
}

/** Everything behind sign-in: the layout and its routes. */
export function AppRoutes() {
  const me = useMe();
  switch (me.state) {
    case 'loading':
      return <p role="status">Loading…</p>;
    case 'signedOut':
      return <SignInPlaceholder />;
    case 'error':
      return <Banner kind="error">Could not reach the server.</Banner>;
    case 'signedIn':
      return (
        <Layout>
          <Routes>
            <Route path="/" element={<ClusterPicker />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="/c/:cluster/*" element={<ClusterPage />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </Layout>
      );
  }
}
