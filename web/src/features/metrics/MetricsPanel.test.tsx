import { screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { fakeClient, jsonResponse, renderApp } from '../../test/render';
import { MetricsPanel } from './MetricsPanel';

const node = {
  metadata: { name: 'n1' },
  status: { allocatable: { cpu: '4', memory: '8Gi' } },
};
const pod = { metadata: { name: 'web', namespace: 'ns' } };

describe('MetricsPanel', () => {
  it('a node shows its usage against what it can allocate', async () => {
    renderApp(<MetricsPanel cluster="v:prod" kind="Node" object={node} />, {
      client: fakeClient({
        'GET /clusters/v%3Aprod/metrics/nodes': () =>
          jsonResponse({
            available: true,
            items: [{ name: 'n1', cpuMillicores: 1000, memoryBytes: 2 * 1024 ** 3 }],
          }),
        'GET /clusters/v%3Aprod/metrics/history?kind=node&name=n1&minutes=60': () =>
          jsonResponse({ available: false, series: [] }),
      }),
    });
    const cpu = await screen.findByRole('meter', { name: 'CPU' });
    expect(cpu).toHaveAttribute('aria-valuenow', '25');
    expect(screen.getByRole('meter', { name: 'Memory' })).toHaveAttribute('aria-valuenow', '25');
    expect(screen.getByText('1 of 4 (25%)')).toBeInTheDocument();
    expect(screen.getByText('2Gi of 8Gi (25%)')).toBeInTheDocument();
  });

  it('a pod shows its numbers and, with Prometheus, the history of CPU and memory', async () => {
    renderApp(<MetricsPanel cluster="v:prod" kind="Pod" object={pod} />, {
      client: fakeClient({
        'GET /clusters/v%3Aprod/metrics/pods?namespace=ns': () =>
          jsonResponse({
            available: true,
            items: [{ namespace: 'ns', name: 'web', cpuMillicores: 250, memoryBytes: 134217728 }],
          }),
        'GET /clusters/v%3Aprod/metrics/history?kind=pod&name=web&minutes=60&namespace=ns': () =>
          jsonResponse({
            available: true,
            series: [
              {
                metric: 'cpu',
                points: [
                  [1, 1],
                  [2, 2],
                ],
              },
              {
                metric: 'memory',
                points: [
                  [1, 5],
                  [2, 4],
                ],
              },
            ],
          }),
      }),
    });
    expect(await screen.findByText('CPU 250m, memory 128Mi')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole('img', { name: 'CPU, last 60 minutes' })).toBeInTheDocument(),
    );
    expect(screen.getByRole('img', { name: 'Memory, last 60 minutes' })).toBeInTheDocument();
  });

  it('shows nothing without metrics, and nothing for other kinds', async () => {
    const { container } = renderApp(<MetricsPanel cluster="v:prod" kind="Pod" object={pod} />, {
      client: fakeClient({
        'GET /clusters/v%3Aprod/metrics/pods?namespace=ns': () =>
          jsonResponse({ available: false, items: [] }),
        'GET /clusters/v%3Aprod/metrics/history?kind=pod&name=web&minutes=60&namespace=ns': () =>
          jsonResponse({ available: false, series: [] }),
      }),
    });
    await waitFor(() => expect(container.querySelector('section')?.textContent).toBe(''));
    expect(screen.queryByRole('meter')).toBeNull();
    const other = renderApp(<MetricsPanel cluster="v:prod" kind="Deployment" object={pod} />);
    expect(other.container.querySelector('section')).toBeNull();
  });
});
