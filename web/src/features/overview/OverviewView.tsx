import type { K8sObject } from '../../state/live';
import { FailingPods } from './FailingPods';
import { FailureCounts, type FailureCountsProps } from './FailureCounts';
import { NodeHealth } from './NodeHealth';
import { NotReadyPods } from './NotReadyPods';
import { PendingPods } from './PendingPods';
import { failingPods, nodeHealth, notReadyPods, pendingPods, warningTimeline } from './triage';
import { WarningTimeline } from './WarningTimeline';

export interface OverviewViewProps extends Omit<FailureCountsProps, 'cluster'> {
  cluster: string;
  now: number;
  /** A list that is not given is not shown: the user may not list it, or it is not there. */
  pods?: K8sObject[];
  nodes?: K8sObject[];
  events?: K8sObject[];
}

/** The triage page of a cluster: what is broken, pending or not ready, and what the modules report. */
export function OverviewView({
  cluster,
  now,
  pods,
  nodes,
  events,
  flux,
  helm,
  vaultSecrets,
  karpenter,
}: OverviewViewProps) {
  const hasFailures =
    flux !== undefined || helm !== undefined || vaultSecrets !== undefined || karpenter !== undefined;
  return (
    <>
      <h1>Overview</h1>
      {nodes && <NodeHealth cluster={cluster} counts={nodeHealth(nodes)} />}
      {pods && <FailingPods cluster={cluster} pods={failingPods(pods)} />}
      {pods && <PendingPods cluster={cluster} pods={pendingPods(pods, events ?? [])} />}
      {pods && <NotReadyPods cluster={cluster} pods={notReadyPods(pods)} />}
      {events && <WarningTimeline buckets={warningTimeline(events, now)} />}
      {hasFailures && (
        <FailureCounts
          cluster={cluster}
          flux={flux}
          helm={helm}
          vaultSecrets={vaultSecrets}
          karpenter={karpenter}
        />
      )}
    </>
  );
}
