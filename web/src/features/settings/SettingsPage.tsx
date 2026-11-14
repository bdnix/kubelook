import { useSettings } from '../../api/settings';
import type { Me } from '../../api/me';
import { ErrorText } from '../../components/ErrorText';
import { themeStore, type Theme } from '../../state/theme';
import { AddCredentials } from '../credentials/AddCredentials';
import { SavedCredentials } from '../credentials/SavedCredentials';
import { ColumnPresets } from './ColumnPresets';
import { NewTokenBox } from './NewTokenBox';
import { WatchIdleSetting } from './WatchIdleSetting';

/** The user's own settings: appearance, live updates, columns and credentials. There are no admin settings here. */
export function SettingsPage({ me }: { me: Me }) {
  const { settings, update, error } = useSettings();
  const setTheme = (theme: Theme) => {
    themeStore.getState().setTheme(theme);
    void update((s) => ({ ...s, theme }));
  };
  return (
    <>
      <h1>Settings</h1>
      <ErrorText message={error} />
      {settings && (
        <>
          <section aria-label="Appearance">
            <h2>Appearance</h2>
            <label>
              Theme
              <select value={settings.theme} onChange={(e) => setTheme(e.target.value as Theme)}>
                <option value="system">System</option>
                <option value="light">Light</option>
                <option value="dark">Dark</option>
              </select>
            </label>
          </section>
          <section aria-label="Live updates">
            <h2>Live updates</h2>
            <WatchIdleSetting
              seconds={settings.watchIdleSeconds}
              onChange={(watchIdleSeconds) => void update((s) => ({ ...s, watchIdleSeconds }))}
            />
          </section>
          <section aria-label="Columns">
            <h2>Columns</h2>
            <ColumnPresets
              presets={settings.columnPresets}
              onChange={(columnPresets) => void update((s) => ({ ...s, columnPresets }))}
            />
          </section>
        </>
      )}
      {me.features.vault && <NewTokenBox />}
      <AddCredentials features={me.features} />
      <SavedCredentials />
    </>
  );
}
