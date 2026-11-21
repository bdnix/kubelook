import { describe, expect, it } from 'vitest';
import { vaultSecretDescriptors } from './vaultsecrets';

describe('VaultSecret descriptor', () => {
  const d = vaultSecretDescriptors()[0];
  const col = (id: string, o: unknown) => d?.columns.find((c) => c.id === id)?.value(o as never, 0);
  it('has the columns of the spec', () => {
    expect(d?.columns.map((c) => c.title)).toEqual([
      'Name',
      'Namespace',
      'Condition',
      'Reason',
      'Path(s)',
      'Engine',
      'Secret',
      'Age',
    ]);
    expect(d?.group).toBe('ricoberger.de');
  });
  it('reads the condition, paths, engine and secret', () => {
    const o = {
      metadata: { name: 'db' },
      spec: { paths: ['kv/a', 'kv/b'], secretEngine: 'kv' },
      status: { conditions: [{ type: 'SecretCreated', status: 'False', reason: 'FetchFailed' }] },
    };
    expect(col('condition', o)).toBe('False');
    expect(col('reason', o)).toBe('FetchFailed');
    expect(col('paths', o)).toBe('kv/a, kv/b');
    expect(col('engine', o)).toBe('kv');
    expect(col('secret', o)).toBe('db');
    expect(col('paths', { spec: { path: 'kv/x' } })).toBe('kv/x');
    expect(col('secret', { metadata: { name: 'db' }, spec: { name: 'generated' } })).toBe('generated');
  });
  it('shows a failed secret as an error', () => {
    expect(
      d?.status?.({ status: { conditions: [{ type: 'SecretCreated', status: 'False' }] } }).severity,
    ).toBe('error');
    expect(
      d?.status?.({ status: { conditions: [{ type: 'SecretCreated', status: 'True' }] } }).severity,
    ).toBe('ok');
  });
});
