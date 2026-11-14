import { describe, expect, it } from 'vitest';
import { kubectlCommand, quote } from './kubectlCommand';

describe('quote', () => {
  it.each([
    ['web-1', 'web-1'],
    ['a.b/c:d', 'a.b/c:d'],
    ['', "''"],
    ['my pod', "'my pod'"],
    ["it's", "'it'\\''s'"],
    ['$(id)', "'$(id)'"],
    ['a;b', "'a;b'"],
    ['-rf', '-rf'],
    ['line\nbreak', "'line\nbreak'"],
  ])('%j → %s', (input, want) => expect(quote(input)).toBe(want));
});

describe('kubectlCommand', () => {
  it.each([
    [{ type: 'replace', namespace: 'default' }, 'kubectl replace -f - -n default'],
    [{ type: 'replace' }, 'kubectl replace -f -'],
    [
      { type: 'delete', resource: 'pod', name: 'web-1', namespace: 'default' },
      'kubectl delete pod web-1 -n default',
    ],
    [{ type: 'delete', resource: 'node', name: 'n1' }, 'kubectl delete node n1'],
    [
      { type: 'scale', resource: 'deployment', name: 'web', replicas: 3, namespace: 'default' },
      'kubectl scale deployment/web --replicas=3 -n default',
    ],
    [
      { type: 'restart', resource: 'deployment', name: 'web', namespace: 'a' },
      'kubectl rollout restart deployment/web -n a',
    ],
    [
      { type: 'pause', resource: 'deployment', name: 'web', namespace: 'a' },
      'kubectl rollout pause deployment/web -n a',
    ],
    [
      { type: 'resume', resource: 'deployment', name: 'web', namespace: 'a' },
      'kubectl rollout resume deployment/web -n a',
    ],
    [
      { type: 'rollback', resource: 'deployment', name: 'web', revision: 4, namespace: 'a' },
      'kubectl rollout undo deployment/web --to-revision=4 -n a',
    ],
    [
      {
        type: 'setImage',
        resource: 'deployment',
        name: 'web',
        container: 'app',
        image: 'nginx:1.27',
        namespace: 'a',
      },
      'kubectl set image deployment/web app=nginx:1.27 -n a',
    ],
    [
      { type: 'cronjobTrigger', name: 'nightly', namespace: 'a' },
      'kubectl create job nightly-manual --from=cronjob/nightly -n a',
    ],
    [
      { type: 'cronjobSuspend', name: 'nightly', namespace: 'a' },
      `kubectl patch cronjob nightly -p '{"spec":{"suspend":true}}' -n a`,
    ],
    [
      { type: 'cronjobResume', name: 'nightly' },
      `kubectl patch cronjob nightly -p '{"spec":{"suspend":false}}'`,
    ],
    [
      { type: 'expandPVC', name: 'data', storage: '20Gi', namespace: 'a' },
      `kubectl patch pvc data -p '{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}' -n a`,
    ],
  ] as const)('%j', (action, want) => expect(kubectlCommand(action)).toBe(want));

  it('evict goes through the eviction subresource', () => {
    expect(kubectlCommand({ type: 'evict', name: 'web-1', namespace: 'a' })).toBe(
      `echo '{"apiVersion":"policy/v1","kind":"Eviction","metadata":{"name":"web-1","namespace":"a"}}' | kubectl create --raw /api/v1/namespaces/a/pods/web-1/eviction -f -`,
    );
  });

  it('quotes names with spaces and quotes', () => {
    expect(kubectlCommand({ type: 'delete', resource: 'pod', name: "it's a pod", namespace: 'my ns' })).toBe(
      `kubectl delete pod 'it'\\''s a pod' -n 'my ns'`,
    );
    expect(
      kubectlCommand({ type: 'scale', resource: 'deployment', name: 'a b', replicas: 1, namespace: 'x' }),
    ).toBe("kubectl scale 'deployment/a b' --replicas=1 -n x");
  });
});
