import { useEffect, useMemo } from 'react';
import { Route, Routes, useParams } from 'react-router';
import { useMe, type Me } from '../../api/me';
import { useSettings } from '../../api/settings';
import { useStatus, type ServerStatus } from '../../api/status';
import { themeStore } from '../../state/theme';
import { Banner } from '../../components/Banner';
import { SignInPage } from '../auth/SignInPage';
import { ClusterPage } from '../clusters/ClusterPage';
import { ClusterRail } from '../clusters/ClusterRail';
import { useClusters } from '../clusters/useClusters';
import { SmallScreenBanner } from '../../components/SmallScreenBanner';
import { CommandPalette } from '../palette/CommandPalette';
import { DockPage } from '../dock/DockPage';
import { HotkeyHost } from './HotkeyHost';
import { Dock } from '../dock/Dock';
import { SettingsPage } from '../settings/SettingsPage';
import { JobsPanel } from '../jobs/JobsPanel';
import { DrawerRoute } from '../details/DrawerRoute';
import { AddCredentials } from '../credentials/AddCredentials';
import { VaultStoreContext } from '../credentials/context';
import { useRestore, type RestoreState } from '../credentials/useRestore';
import { openVaultStore } from '../credentials/vaultStore';
import { Banners } from './Banners';
import { Layout } from './Layout';
import { SignOutButton } from './SignOutButton';
import { ResourceSidebar } from './Sidebar';

function Home({
  me,
  restore,
  hasClusters,
}: {
  me: Me;
  restore: RestoreState;
  hasClusters: boolean | undefined;
}) {
  if (restore.state === 'restoring' || hasClusters === undefined) return <p role="status">Loading…</p>;
  if (!hasClusters) {
    return (
      <>
        <h1>Add your credentials</h1>
        <p>Add a Vault token or a kubeconfig to see your clusters.</p>
        <AddCredentials features={me.features} />
      </>
    );
  }
  return <h1>Choose a cluster</h1>;
}

function SidebarRoute() {
  const { cluster = '' } = useParams();
  return <ResourceSidebar cluster={cluster} />;
}

function NotFound() {
  return <p>Page not found</p>;
}

/** The signed-in app: restores saved credentials, then shows the cluster rail, banners and pages. */
function SignedIn({ me }: { me: Me }) {
  const restore = useRestore();
  const clusters = useClusters();
  const { update } = useSettings();
  // The theme the user saved on the server follows them to this browser.
  const savedTheme = me.settings.theme;
  useEffect(() => {
    if (savedTheme === 'light' || savedTheme === 'dark' || savedTheme === 'system')
      themeStore.getState().setTheme(savedTheme);
  }, [savedTheme]);
  const toggleFavourite = (id: string, on: boolean) =>
    void update((s) => ({
      ...s,
      favouriteClusters: on
        ? [...s.favouriteClusters.filter((f) => f !== id), id]
        : s.favouriteClusters.filter((f) => f !== id),
    }));
  const sources = clusters.data;
  // A failed list reads as "no clusters": the user can still add credentials.
  const hasClusters = clusters.isPending ? undefined : (sources?.length ?? 0) > 0;
  const status = useStatus();
  const vault = status.data?.vault as ServerStatus['vault'] | null | undefined;
  const vaultDown = vault?.configured === true && !vault.reachable;
  return (
    <Layout
      clusterRail={
        <ClusterRail
          sources={sources ?? []}
          favourites={me.settings.favouriteClusters ?? []}
          onToggleFavourite={toggleFavourite}
          vaultUnreachable={vaultDown}
        />
      }
      resourceNav={
        <Routes>
          <Route path="/c/:cluster/*" element={<SidebarRoute />} />
        </Routes>
      }
      drawer={
        <Routes>
          <Route path="/c/:cluster/ns/:namespace/:group/:kind/:name" element={<DrawerRoute />} />
          <Route path="/c/:cluster/:group/:kind/:name" element={<DrawerRoute />} />
        </Routes>
      }
      dock={
        <>
          <JobsPanel />
          <Dock liveBufferLines={me.limits.logsLiveBufferLines} />
        </>
      }
    >
      <SmallScreenBanner />
      <SignOutButton />
      <CommandPalette />
      <HotkeyHost />
      <Banners
        maintenanceBanner={me.maintenanceBanner}
        sources={sources}
        vaultWarningSeconds={me.limits.vaultExpiryWarningSeconds}
        vaultUnreachable={vaultDown}
        notices={restore.notices}
      />
      <Routes>
        <Route path="/" element={<Home me={me} restore={restore} hasClusters={hasClusters} />} />
        <Route path="/settings" element={<SettingsPage me={me} />} />
        <Route path="/c/:cluster/*" element={<ClusterPage />} />
        <Route path="*" element={<NotFound />} />
      </Routes>
    </Layout>
  );
}

/** Everything behind sign-in: the layout and its routes. */
export function AppRoutes() {
  const me = useMe();
  const userId = me.state === 'signedIn' ? me.me.user.id : undefined;
  const store = useMemo(() => (userId === undefined ? null : openVaultStore(userId)), [userId]);
  switch (me.state) {
    case 'loading':
      return <p role="status">Loading…</p>;
    case 'signedOut':
      return <SignInPage />;
    case 'error':
      return <Banner kind="error">Could not reach the server.</Banner>;
    case 'signedIn':
      return (
        <VaultStoreContext.Provider value={store}>
          <Routes>
            <Route path="/dock" element={<DockPage />} />
            <Route path="*" element={<SignedIn me={me.me} />} />
          </Routes>
        </VaultStoreContext.Provider>
      );
  }
}
