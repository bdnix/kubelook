import { useEffect } from 'react';
import { useMe } from '../../api/me';
import { dockStore } from '../../state/dock';
import { Dock } from './Dock';
import { browserChannel, createPopoutPage, DOCK_CHANNEL } from './popout';

/** `/dock`: the dock in a window of its own. It takes over the tabs of the main window and returns them on close. */
export function DockPage() {
  const me = useMe();
  useEffect(() => {
    const channel = browserChannel(DOCK_CHANNEL);
    let hide: () => void = () => undefined;
    createPopoutPage({ store: dockStore, channel, onPageHide: (fn) => (hide = fn) });
    const onHide = () => hide();
    window.addEventListener('pagehide', onHide);
    return () => {
      window.removeEventListener('pagehide', onHide);
      channel.close();
    };
  }, []);
  const lines = me.state === 'signedIn' ? me.me.limits.logsLiveBufferLines : 10000;
  return <Dock liveBufferLines={lines} canPopOut={false} />;
}
