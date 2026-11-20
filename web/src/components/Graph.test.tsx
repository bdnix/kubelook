import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { Graph, type GraphEdge, type GraphNode } from './Graph';

const nodes: GraphNode[] = [
  { id: 'git', kind: 'GitRepository', name: 'repo', status: 'ok', facts: ['Revision main@sha1:abc'] },
  { id: 'ks', kind: 'Kustomization', name: 'apps', status: 'error', facts: ['Ready=False: build failed'] },
];
const edges: GraphEdge[] = [{ from: 'git', to: 'ks' }];

const transform = () =>
  document.querySelector('[data-testid="graph-content"]')?.getAttribute('transform') ?? '';

describe('Graph', () => {
  it('draws every node as a focusable button named by kind, name and status', () => {
    render(<Graph label="Flux graph" nodes={nodes} edges={edges} onOpen={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'GitRepository repo, ready' })).toHaveAttribute(
      'tabindex',
      '0',
    );
    expect(screen.getByRole('button', { name: 'Kustomization apps, failed' })).toBeInTheDocument();
    expect(document.querySelectorAll('line')).toHaveLength(1);
  });

  it('opens a node on click, Enter and Space', async () => {
    const onOpen = vi.fn();
    render(<Graph label="g" nodes={nodes} edges={edges} onOpen={onOpen} />);
    const node = screen.getByRole('button', { name: /Kustomization apps/ });
    await userEvent.click(node);
    expect(onOpen).toHaveBeenLastCalledWith(nodes[1]);
    node.focus();
    await userEvent.keyboard('{Enter}');
    await userEvent.keyboard(' ');
    expect(onOpen).toHaveBeenCalledTimes(3);
  });

  it('shows the facts of a node while it is hovered or focused', async () => {
    render(<Graph label="g" nodes={nodes} edges={edges} onOpen={vi.fn()} />);
    expect(screen.queryByRole('tooltip')).toBeNull();
    await userEvent.hover(screen.getByRole('button', { name: /Kustomization apps/ }));
    expect(screen.getByRole('tooltip')).toHaveTextContent('Ready=False: build failed');
    await userEvent.unhover(screen.getByRole('button', { name: /Kustomization apps/ }));
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.focus(screen.getByRole('button', { name: /GitRepository repo/ }));
    expect(screen.getByRole('tooltip')).toHaveTextContent('Revision main@sha1:abc');
  });

  it('zooms with the wheel, within limits', () => {
    render(<Graph label="g" nodes={nodes} edges={edges} onOpen={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'g' });
    const before = transform();
    fireEvent.wheel(svg, { deltaY: -100 });
    expect(transform()).not.toBe(before);
    expect(transform()).toMatch(/scale\(1\.\d+\)/);
    for (let i = 0; i < 100; i++) fireEvent.wheel(svg, { deltaY: -100 });
    expect(transform()).toMatch(/scale\(4\)/);
    for (let i = 0; i < 200; i++) fireEvent.wheel(svg, { deltaY: 100 });
    expect(transform()).toMatch(/scale\(0\.2\)/);
  });

  it('pans when the background is dragged', () => {
    render(<Graph label="g" nodes={nodes} edges={edges} onOpen={vi.fn()} />);
    const svg = screen.getByRole('img', { name: 'g' });
    const before = transform();
    fireEvent.pointerDown(svg, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerMove(svg, { clientX: 60, clientY: 40, pointerId: 1 });
    fireEvent.pointerUp(svg, { pointerId: 1 });
    expect(transform()).not.toBe(before);
    expect(transform()).toMatch(/translate\(50/);
  });

  it('moves a node when it is dragged, and does not open it', () => {
    const onOpen = vi.fn();
    render(<Graph label="g" nodes={nodes} edges={edges} onOpen={onOpen} />);
    const node = screen.getByRole('button', { name: /GitRepository repo/ });
    const at = () => node.getAttribute('transform');
    const before = at();
    fireEvent.pointerDown(node, { clientX: 0, clientY: 0, pointerId: 1 });
    fireEvent.pointerMove(node, { clientX: 80, clientY: 30, pointerId: 1 });
    fireEvent.pointerUp(node, { pointerId: 1 });
    fireEvent.click(node);
    expect(at()).not.toBe(before);
    expect(onOpen).not.toHaveBeenCalled();
  });

  it('says so when there is nothing to draw', () => {
    render(<Graph label="g" nodes={[]} edges={[]} onOpen={vi.fn()} />);
    expect(screen.getByText('Nothing to show.')).toBeInTheDocument();
  });

  it('renders cluster-supplied names as text', () => {
    render(
      <Graph
        label="g"
        nodes={[{ id: 'x', kind: 'Kustomization', name: '<img src=x onerror=alert(1)>', status: 'unknown' }]}
        edges={[]}
        onOpen={vi.fn()}
      />,
    );
    expect(screen.getByRole('button', { name: /<img src=x/ })).toBeInTheDocument();
    expect(document.querySelector('img')).toBeNull();
  });
});
