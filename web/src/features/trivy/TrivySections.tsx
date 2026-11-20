import { useResourceInfo } from '../details/useDetails';
import { useLive } from '../../state/useLive';
import type { K8sObject } from '../../state/live';
import { useModules } from '../modules/useModules';
import { reportsForWorkload } from './findings';
import { ReportDetails } from './ReportDetails';
import type { ReportRef } from './TrivyOverview';
import { WorkloadTrivySection } from './WorkloadTrivySection';

const GROUP = 'aquasecurity.github.io';

/** The findings section of the Overview of a Trivy report; it needs the version the cluster serves. */
export function TrivyReportSection({
  cluster,
  kind,
  object,
}: {
  cluster: string;
  kind: string;
  object: K8sObject;
}) {
  const info = useResourceInfo(cluster, GROUP, kind);
  if (!info) return null;
  return (
    <ReportDetails
      cluster={cluster}
      kind={kind}
      group={GROUP}
      version={info.version}
      resource={info.resource}
      object={object}
    />
  );
}

const WORKLOAD_REPORTS = ['VulnerabilityReport', 'ConfigAuditReport', 'ExposedSecretReport'];

/** The Trivy reports about a workload, when the Trivy module is active. */
export function WorkloadTrivy({
  cluster,
  kind,
  name,
  namespace,
}: {
  cluster: string;
  kind: string;
  name: string;
  namespace: string;
}) {
  const modules = useModules(cluster);
  const active = modules?.find((m) => m.name === 'trivy')?.active === true;
  const served = modules?.find((m) => m.name === 'trivy')?.kinds ?? [];
  const key = (k: string) => {
    const s = served.find((x) => x.kind === k);
    return active && s
      ? { cluster, group: s.group, version: s.version, resource: s.resource, namespace }
      : undefined;
  };
  const vulnerabilities = useLive(key(WORKLOAD_REPORTS[0] as string));
  const audits = useLive(key(WORKLOAD_REPORTS[1] as string));
  const secrets = useLive(key(WORKLOAD_REPORTS[2] as string));
  const refs: ReportRef[] = [
    ...vulnerabilities.items.map((object) => ({ kind: 'VulnerabilityReport', group: GROUP, object })),
    ...audits.items.map((object) => ({ kind: 'ConfigAuditReport', group: GROUP, object })),
    ...secrets.items.map((object) => ({ kind: 'ExposedSecretReport', group: GROUP, object })),
  ];
  const mine = reportsForWorkload(
    refs.map((r) => r.object),
    { kind, name, namespace },
  );
  return <WorkloadTrivySection cluster={cluster} reports={refs.filter((r) => mine.includes(r.object))} />;
}
