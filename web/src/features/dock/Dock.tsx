import { LogViewer } from '../logs/LogViewer';
import { ShellTab } from '../shell/ShellTab';
import { useDock } from '../../state/dock';

/**
 * The tabs of the bottom dock: one per open log (more kinds follow). Tabs stay mounted so their
 * streams keep running. The layout provides the region named "Dock" that holds them.
 */
export function Dock({ liveBufferLines }: { liveBufferLines: number }) {
  const { tabs, active, activate, close } = useDock();
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
          ) : (
            <ShellTab {...t.props} />
          )}
        </div>
      ))}
    </div>
  );
}
