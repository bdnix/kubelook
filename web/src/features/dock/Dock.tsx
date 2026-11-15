import { LogViewer } from '../logs/LogViewer';
import { ShellTab } from '../shell/ShellTab';
import { useMemo } from 'react';
import { Button } from '../../components/Button';
import { browserChannel, createPopout, DOCK_CHANNEL } from './popout';
import { dockStore, shellTab, useDock } from '../../state/dock';

/**
 * The tabs of the bottom dock: one per open log (more kinds follow). Tabs stay mounted so their
 * streams keep running. The layout provides the region named "Dock" that holds them.
 */
export function Dock({
  liveBufferLines,
  canPopOut = true,
}: {
  liveBufferLines: number;
  canPopOut?: boolean;
}) {
  const { tabs, active, activate, close, open } = useDock();
  const popout = useMemo(
    () =>
      canPopOut && typeof BroadcastChannel !== 'undefined'
        ? createPopout({
            store: dockStore,
            openWindow: (url, name, features) => window.open(url, name, features),
            channel: browserChannel(DOCK_CHANNEL),
          })
        : undefined,
    [canPopOut],
  );
  if (tabs.length === 0) return null;
  return (
    <div className="dock">
      <div role="tablist" aria-label="Dock tabs">
        {tabs.map((t) => (
          <span key={t.id} className="dock-tab">
            <button
              type="button"
              role="tab"
              id={`tab-${t.id}`}
              aria-selected={t.id === active}
              aria-controls={`panel-${t.id}`}
              onClick={() => activate(t.id)}
            >
              {t.title}
            </button>
            <button type="button" aria-label={`Close ${t.title}`} onClick={() => close(t.id)}>
              ×
            </button>
          </span>
        ))}
      </div>
      {popout && active && <Button onClick={() => popout.popOut([active])}>Pop out</Button>}
      {tabs.map((t) => (
        <div
          key={t.id}
          role="tabpanel"
          id={`panel-${t.id}`}
          aria-labelledby={`tab-${t.id}`}
          hidden={t.id !== active}
        >
          {t.kind === 'logs' ? (
            <LogViewer {...t.props} maxLines={liveBufferLines} />
          ) : t.props.moved ? (
            <div>
              <p>A shell cannot move between windows.</p>
              <Button
                onClick={() => {
                  const { moved: _moved, ...props } = t.props;
                  void _moved;
                  close(t.id);
                  open(shellTab(props, Date.now()));
                }}
              >
                Start a new shell
              </Button>
            </div>
          ) : (
            <ShellTab {...t.props} />
          )}
        </div>
      ))}
    </div>
  );
}
