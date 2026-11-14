import { lazy, Suspense } from 'react';
import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { useShell, type ShellRequest } from './useShell';

const Terminal = lazy(() => import('../../adapters/xterm/Terminal').then((m) => ({ default: m.Terminal })));

/** A shell in a container: the terminal and, once it has ended, why and a way to start a new one. */
export function ShellTab(request: ShellRequest) {
  const shell = useShell(request);
  const ended = shell.status === 'lost' || shell.status === 'exited' || shell.status === 'error';
  return (
    <div className="shell">
      {shell.status === 'lost' && <Banner kind="error">{shell.error}</Banner>}
      {shell.status === 'error' && shell.error && <Banner kind="error">{shell.error}</Banner>}
      {shell.status === 'exited' && (
        <Banner kind="info">{`The shell ended${shell.exitCode ? ` with exit code ${shell.exitCode}` : ''}.`}</Banner>
      )}
      {ended && <Button onClick={shell.restart}>Start a new shell</Button>}
      {shell.status === 'connecting' && <p role="status">Starting the shell…</p>}
      <Suspense fallback={<p role="status">Loading the terminal…</p>}>
        <Terminal label={`Shell in ${request.container}`} onReady={shell.attach} />
      </Suspense>
    </div>
  );
}
