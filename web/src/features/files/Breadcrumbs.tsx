import { Button } from '../../components/Button';

/** The path as buttons: the root and every directory above the current one. */
export function Breadcrumbs({ path, onNavigate }: { path: string; onNavigate: (path: string) => void }) {
  const parts = path.split('/').filter((p) => p !== '');
  return (
    <nav aria-label="Path">
      <ol>
        <li>
          <Button onClick={() => onNavigate('/')} aria-current={parts.length === 0 ? 'page' : undefined}>
            /
          </Button>
        </li>
        {parts.map((part, i) => {
          const to = `/${parts.slice(0, i + 1).join('/')}`;
          return (
            <li key={to}>
              <Button
                onClick={() => onNavigate(to)}
                aria-current={i === parts.length - 1 ? 'page' : undefined}
              >
                {part}
              </Button>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
