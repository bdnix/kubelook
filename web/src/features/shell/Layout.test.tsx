import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../../test/render';
import { Layout } from './Layout';

describe('Layout', () => {
  it('exposes the four areas as landmarks', () => {
    renderApp(<Layout>content</Layout>);
    expect(screen.getByRole('navigation', { name: 'Clusters' })).toBeInTheDocument();
    expect(screen.getByRole('navigation', { name: 'Resources' })).toBeInTheDocument();
    expect(screen.getByRole('main')).toHaveTextContent('content');
    expect(screen.getByRole('region', { name: 'Dock' })).toBeInTheDocument();
  });

  it('has no details complementary until a drawer is open', () => {
    renderApp(<Layout>content</Layout>);
    expect(screen.queryByRole('complementary', { name: 'Details' })).toBeNull();
  });

  it('shows the details drawer when given one', () => {
    renderApp(<Layout drawer={<p>drawer body</p>}>content</Layout>);
    expect(screen.getByRole('complementary', { name: 'Details' })).toHaveTextContent('drawer body');
  });

  it('renders the slots it is given', () => {
    renderApp(
      <Layout clusterRail={<span>rail</span>} resourceNav={<span>nav</span>} dock={<span>dock</span>}>
        content
      </Layout>,
    );
    expect(screen.getByRole('navigation', { name: 'Clusters' })).toHaveTextContent('rail');
    expect(screen.getByRole('navigation', { name: 'Resources' })).toHaveTextContent('nav');
    expect(screen.getByRole('region', { name: 'Dock' })).toHaveTextContent('dock');
  });
});
