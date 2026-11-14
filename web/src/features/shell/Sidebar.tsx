import { NavLink } from 'react-router';
import { findResource, useDiscovery, type Discovery } from '../../api/discovery';
import { useAccess, type AccessCheck } from '../../api/access';
import { knownKinds } from '../../kinds/registry';
import type { KindDescriptor } from '../../kinds/types';
import { clusterPath } from '../../lib/paths';

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
  for (const p of PAGES) {
    const items = bySection.get(p.section) ?? [];
    items.push({ key: p.path, title: p.title, to: `${clusterPath(cluster)}/${p.path}` });
    bySection.set(p.section, items);
  }
  return (
    <>
      {SECTIONS.filter((s) => (bySection.get(s)?.length ?? 0) > 0).map((section) => (
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
      ))}
    </>
  );
}

function listChecks(discovery: Discovery | undefined): { kind: KindDescriptor; check: AccessCheck }[] {
  if (!discovery) return [];
  const out: { kind: KindDescriptor; check: AccessCheck }[] = [];
  for (const kind of knownKinds()) {
    const served = findResource(discovery, kind.group, kind.kind);
    if (served) out.push({ kind, check: { verb: 'list', group: served.group, resource: served.resource } });
  }
  return out;
}

/** The sidebar of an open cluster, with discovery and the access checks behind it. */
export function ResourceSidebar({ cluster }: { cluster: string }) {
  const discovery = useDiscovery(cluster);
  const checks = listChecks(discovery.data);
  const access = useAccess(
    cluster,
    checks.map((c) => c.check),
  );
  const canList = (group: string, resource: string): boolean | undefined => {
    const i = checks.findIndex((c) => c.check.group === group && c.check.resource === resource);
    return i < 0 ? undefined : access.data?.[i]?.allowed;
  };
  return <Sidebar cluster={cluster} discovery={discovery.data} canList={canList} />;
}
