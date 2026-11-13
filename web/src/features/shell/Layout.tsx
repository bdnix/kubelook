import type { ReactNode } from 'react';
import './layout.css';

export interface LayoutProps {
  clusterRail?: ReactNode;
  resourceNav?: ReactNode;
  drawer?: ReactNode;
  dock?: ReactNode;
  children: ReactNode;
}

/** The four areas every view lives in: cluster rail, resource navigation, main, details drawer and dock. */
export function Layout({ clusterRail, resourceNav, drawer, dock, children }: LayoutProps) {
  return (
    <div className="layout">
      <nav aria-label="Clusters" className="layout-clusters">
        {clusterRail}
      </nav>
      <nav aria-label="Resources" className="layout-resources">
        {resourceNav}
      </nav>
      <main className="layout-main">{children}</main>
      {drawer && (
        <aside aria-label="Details" className="layout-drawer">
          {drawer}
        </aside>
      )}
      <section aria-label="Dock" className="layout-dock">
        {dock}
      </section>
    </div>
  );
}
