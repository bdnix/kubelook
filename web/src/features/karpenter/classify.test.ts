import { describe, expect, it } from 'vitest';
import { classifyEvent } from './classify';

const e = (reason: string, type = 'Normal', message = '') => ({ type, reason, message });

describe('classifyEvent', () => {
  it.each([
    ['DisruptionBlocked', 'Normal', 'scaleDown'],
    ['Terminating', 'Normal', 'scaleDown'],
    ['Consolidating', 'Normal', 'scaleDown'],
    ['Evicted', 'Normal', 'scaleDown'],
    ['Empty', 'Normal', 'scaleDown'],
    ['Expired', 'Normal', 'scaleDown'],
    ['Nominated', 'Normal', 'scaleUp'],
    ['LaunchSucceeded', 'Normal', 'scaleUp'],
    ['Registered', 'Normal', 'scaleUp'],
    ['Initialized', 'Normal', 'scaleUp'],
    ['Provisioning', 'Normal', 'scaleUp'],
    ['DriftDetected', 'Normal', 'drift'],
    ['Drifted', 'Warning', 'drift'],
    ['InsufficientCapacityError', 'Normal', 'errors'],
    ['LaunchFailed', 'Normal', 'errors'],
    ['NodeError', 'Normal', 'errors'],
    ['SomethingElse', 'Warning', 'errors'],
    ['Unconsolidatable', 'Normal', 'scaleDown'],
    ['FullyInformational', 'Normal', 'other'],
  ] as const)('%s (%s) is %s', (reason, type, want) => {
    expect(classifyEvent(e(reason, type))).toBe(want);
  });

  it('ignores case', () => {
    expect(classifyEvent(e('LAUNCHED'))).toBe('scaleUp');
    expect(classifyEvent(e('drift'))).toBe('drift');
  });

  it('puts drift before errors: a drift warning is drift', () => {
    expect(classifyEvent(e('DriftBlocked', 'Warning'))).toBe('drift');
  });
});
