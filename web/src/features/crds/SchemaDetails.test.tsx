import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { createClient } from '../../api/client';
import { jsonResponse, renderApp } from '../../test/render';
import type { CrdLike } from '../../kinds/generic';
import { CustomResourceOverview } from './CustomResourceOverview';
import { SchemaDetails } from './SchemaDetails';

const schema = {
  type: 'object',
  properties: {
    spec: {
      type: 'object',
      properties: {
        replicas: { type: 'integer', description: 'How many copies run.' },
        tls: { type: 'object', properties: { secret: { type: 'string', description: 'The TLS secret.' } } },
        hosts: { type: 'array', items: { type: 'string' } },
        rules: { type: 'array', items: { type: 'object', properties: { path: { type: 'string' } } } },
      },
    },
    status: { type: 'object', properties: { state: { type: 'string', description: 'Current state.' } } },
  },
};

const object = {
  spec: {
    replicas: 3,
    tls: { secret: 'cert' },
    hosts: ['a.example.com', 'b.example.com'],
    rules: [{ path: '/api' }],
    unknown: true,
  },
  status: { state: 'Ready', conditions: [{ type: 'Ready', status: 'True' }] },
};

describe('SchemaDetails', () => {
  it('shows spec and status as a tree, with the description of each field from the schema', () => {
    render(<SchemaDetails schema={schema} object={object} />);
    const spec = within(screen.getByRole('region', { name: 'Spec' }));
    expect(spec.getByText('replicas')).toBeInTheDocument();
    expect(spec.getByText('3')).toBeInTheDocument();
    expect(spec.getByText('How many copies run.')).toBeInTheDocument();
    expect(spec.getByText('secret')).toBeInTheDocument();
    expect(spec.getByText('cert')).toBeInTheDocument();
    expect(spec.getByText('The TLS secret.')).toBeInTheDocument();
    expect(spec.getByText('a.example.com')).toBeInTheDocument();
    expect(spec.getByText('/api')).toBeInTheDocument();
    expect(spec.getByText('unknown')).toBeInTheDocument();
    const status = within(screen.getByRole('region', { name: 'Status' }));
    expect(status.getByText('Current state.')).toBeInTheDocument();
    expect(status.getByText('Ready')).toBeInTheDocument();
  });

  it('leaves the conditions to the conditions table', () => {
    render(<SchemaDetails schema={schema} object={object} />);
    expect(screen.queryByText('conditions')).toBeNull();
  });

  it('works without a schema', () => {
    render(<SchemaDetails schema={undefined} object={object} />);
    expect(screen.getByText('replicas')).toBeInTheDocument();
  });

  it('shows what lies deeper than 8 levels as YAML', () => {
    let deep: Record<string, unknown> = { leaf: 'x' };
    for (let i = 0; i < 10; i++) deep = { n: deep };
    render(<SchemaDetails schema={undefined} object={{ spec: deep }} />);
    expect(screen.getByText(/leaf: x/)).toBeInTheDocument();
  });

  it('renders cluster-supplied text as text, never as HTML', () => {
    render(<SchemaDetails schema={undefined} object={{ spec: { note: '<img src=x onerror=alert(1)>' } }} />);
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeInTheDocument();
    expect(document.querySelector('img')).toBeNull();
  });

  it('has nothing to show for an object without spec and status', () => {
    const { container } = render(<SchemaDetails schema={schema} object={{ metadata: {} }} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('CustomResourceOverview', () => {
  const crd: CrdLike = {
    spec: {
      group: 'example.com',
      names: { kind: 'Widget', plural: 'widgets' },
      scope: 'Namespaced',
      versions: [{ name: 'v1', served: true, schema: { openAPIV3Schema: schema } }],
    },
  };
  const discovery = {
    serverVersion: 'v1.35.0',
    resources: [
      {
        group: 'example.com',
        version: 'v1',
        resource: 'widgets',
        kind: 'Widget',
        namespaced: true,
        verbs: ['list'],
        preferred: true,
        servedVersions: ['v1'],
      },
    ],
  };

  it('reads the CRD and shows the descriptions of the served version', async () => {
    const fetch = vi.fn((input: RequestInfo | URL) =>
      Promise.resolve(String(input).endsWith('/discovery') ? jsonResponse(discovery) : jsonResponse(crd)),
    );
    renderApp(<CustomResourceOverview cluster="c" group="example.com" kind="Widget" object={object} />, {
      client: createClient({ fetch }),
    });
    expect(await screen.findByText('How many copies run.')).toBeInTheDocument();
  });

  it('still shows the values when the CRD cannot be read', async () => {
    const fetch = vi.fn((input: RequestInfo | URL) =>
      Promise.resolve(
        String(input).endsWith('/discovery')
          ? jsonResponse(discovery)
          : jsonResponse({ error: { code: 'forbidden', message: 'no' } }, 403),
      ),
    );
    renderApp(<CustomResourceOverview cluster="c" group="example.com" kind="Widget" object={object} />, {
      client: createClient({ fetch }),
    });
    expect(await screen.findByText('replicas')).toBeInTheDocument();
    expect(screen.queryByText('How many copies run.')).toBeNull();
  });
});
