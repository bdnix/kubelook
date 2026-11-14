export type KubectlAction =
  | { type: 'replace'; namespace?: string }
  | { type: 'delete'; resource: string; name: string; namespace?: string }
  | { type: 'scale'; resource: string; name: string; replicas: number; namespace?: string }
  | { type: 'restart' | 'pause' | 'resume'; resource: string; name: string; namespace?: string }
  | { type: 'rollback'; resource: string; name: string; revision: number; namespace?: string }
  | { type: 'setImage'; resource: string; name: string; container: string; image: string; namespace?: string }
  | { type: 'cronjobTrigger'; name: string; namespace?: string }
  | { type: 'cronjobSuspend' | 'cronjobResume'; name: string; namespace?: string }
  | { type: 'evict'; name: string; namespace: string }
  | { type: 'expandPVC'; name: string; storage: string; namespace?: string };

const SAFE = /^[A-Za-z0-9_@%+=:,./-]+$/;

/** Quotes one shell word so names with spaces, quotes or `$()` cannot change the command. */
export function quote(s: string): string {
  if (s !== '' && SAFE.test(s)) return s;
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

const ns = (namespace: string | undefined) => (namespace ? ` -n ${quote(namespace)}` : '');

/** The `kubectl` command that does what an action does, for people who want to run it themselves. */
export function kubectlCommand(a: KubectlAction): string {
  switch (a.type) {
    case 'replace':
      return `kubectl replace -f -${ns(a.namespace)}`;
    case 'delete':
      return `kubectl delete ${quote(a.resource)} ${quote(a.name)}${ns(a.namespace)}`;
    case 'scale':
      return `kubectl scale ${quote(`${a.resource}/${a.name}`)} --replicas=${a.replicas}${ns(a.namespace)}`;
    case 'restart':
      return `kubectl rollout restart ${quote(`${a.resource}/${a.name}`)}${ns(a.namespace)}`;
    case 'pause':
      return `kubectl rollout pause ${quote(`${a.resource}/${a.name}`)}${ns(a.namespace)}`;
    case 'resume':
      return `kubectl rollout resume ${quote(`${a.resource}/${a.name}`)}${ns(a.namespace)}`;
    case 'rollback':
      return `kubectl rollout undo ${quote(`${a.resource}/${a.name}`)} --to-revision=${a.revision}${ns(a.namespace)}`;
    case 'setImage':
      return `kubectl set image ${quote(`${a.resource}/${a.name}`)} ${quote(`${a.container}=${a.image}`)}${ns(a.namespace)}`;
    case 'cronjobTrigger':
      return `kubectl create job ${quote(`${a.name}-manual`)} --from=${quote(`cronjob/${a.name}`)}${ns(a.namespace)}`;
    case 'cronjobSuspend':
    case 'cronjobResume':
      return `kubectl patch cronjob ${quote(a.name)} -p ${quote(`{"spec":{"suspend":${a.type === 'cronjobSuspend'}}}`)}${ns(a.namespace)}`;
    case 'evict': {
      const body = JSON.stringify({
        apiVersion: 'policy/v1',
        kind: 'Eviction',
        metadata: { name: a.name, namespace: a.namespace },
      });
      return `echo ${quote(body)} | kubectl create --raw ${quote(`/api/v1/namespaces/${a.namespace}/pods/${a.name}/eviction`)} -f -`;
    }
    case 'expandPVC':
      return `kubectl patch pvc ${quote(a.name)} -p ${quote(JSON.stringify({ spec: { resources: { requests: { storage: a.storage } } } }))}${ns(a.namespace)}`;
  }
}
