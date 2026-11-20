import { NavLink } from 'react-router';
import { findResource, useDiscovery, type Discovery } from '../../api/discovery';
import { useAccess, type AccessCheck } from '../../api/access';
import { knownKinds } from '../../kinds/registry';
import { clusterPath } from '../../lib/paths';
import { CrdSidebarSection } from '../crds/CrdSidebarSection';
import { customKinds } from '../crds/customKinds';

/** The sections of the resource sidebar, in the order of the spec. */
export const SECTIONS = [
  'Overview',
  'Workloads',
  'Config',
  'Network',
  'Storage',
  'Cluster',
  'Access Control',
  'Helm',
  'FluxCD',
  'Resource Map',
  'Pod Files',
  'Modules',
  'Custom Resources',
  'Events',
] as const;

interface Item {
  key: string;
  title: string;
  to: string;
}

/** Pages that are not Kubernetes kinds. Later pull requests add Overview, Helm, FluxCD and the others here. */
const PAGES: { section: string; title: string; path: string }[] = [
  { section: 'Access Control', title: 'Who can…', path: 'who-can' },
  { section: 'Pod Files', title: 'Pod Files', path: 'pod-files' },
];

export interface SidebarProps {
  cluster: string;
  /** Undefined while discovery loads. */
  discovery: Discovery | undefined;
  /** Whether the user may list the resource; undefined while the answer is not known (the item is shown). */
  canList: (group: string, resource: string) => boolean | undefined;
}

function kindItems(cluster: string, discovery: Discovery, canList: SidebarProps['canList']) {
  const bySection = new Map<string, Item[]>();
  for (const k of knownKinds()) {
    const served = findResource(discovery, k.group, k.kind);
    if (!served || canList(served.group, served.resource) === false) continue;
    const items = bySection.get(k.section) ?? [];
    items.push({
      key: `${k.group}/${k.kind}`,
      title: k.title,
      to: `${clusterPath(cluster)}/${k.group || 'core'}/${k.kind}`,
    });
    bySection.set(k.section, items);
  }
  return bySection;
}

/** The resource sidebar: sections and kinds the cluster serves and the user may list. */
export function Sidebar({ cluster, discovery, canList }: SidebarProps) {
  if (!discovery) return <p role="status">Loading…</p>;
  const bySection = kindItems(cluster, discovery, canList);
  const custom = customKinds(discovery).filter((k) => canList(k.group, k.resource) !== false);
  for (const p of PAGES) {
    const items = bySection.get(p.section) ?? [];
    items.push({ key: p.path, title: p.title, to: `${clusterPath(cluster)}/${p.path}` });
    bySection.set(p.section, items);
  }
  const has = (section: string) =>
    section === 'Custom Resources' ? custom.length > 0 : (bySection.get(section)?.length ?? 0) > 0;
  return (
    <>
      {SECTIONS.filter(has).map((section) =>
        section === 'Custom Resources' ? (
          <section key={section} aria-label={section} className="sidebar-section">
            <h2>{section}</h2>
            <CrdSidebarSection cluster={cluster} kinds={custom} />
          </section>
        ) : (
          <section key={section} aria-label={section} className="sidebar-section">
            <h2>{section}</h2>
            <ul>
              {bySection.get(section)?.map((item) => (
                <li key={item.key}>
                  <NavLink to={item.to} end>
                    {item.title}
                  </NavLink>
                </li>
              ))}
            </ul>
          </section>
        ),
      )}
    </>
  );
}

function listChecks(discovery: Discovery | undefined): AccessCheck[] {
  if (!discovery) return [];
  const served = [
    ...knownKinds().flatMap((k) => findResource(discovery, k.group, k.kind) ?? []),
    ...customKinds(discovery),
  ];
  return served.map((r) => ({ verb: 'list', group: r.group, resource: r.resource }));
}

/** The sidebar of an open cluster, with discovery and the access checks behind it. */
export function ResourceSidebar({ cluster }: { cluster: string }) {
  const discovery = useDiscovery(cluster);
  const checks = listChecks(discovery.data);
  const access = useAccess(cluster, checks);
  const canList = (group: string, resource: string): boolean | undefined => {
    const i = checks.findIndex((c) => c.group === group && c.resource === resource);
    return i < 0 ? undefined : access.data?.[i]?.allowed;
  };
  return <Sidebar cluster={cluster} discovery={discovery.data} canList={canList} />;
}
