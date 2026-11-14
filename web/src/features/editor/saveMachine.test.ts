import { describe, expect, it } from 'vitest';
import { initialSaveState, saveReducer, type SaveEvent, type SavePhase, type SaveState } from './saveMachine';

const at = (phase: SavePhase, extra: Partial<SaveState> = {}): SaveState => ({
  phase,
  previewUnsupported: false,
  ...extra,
});

const events: Record<string, SaveEvent> = {
  edit: { type: 'edit' },
  previewStart: { type: 'previewStart' },
  previewDone: { type: 'previewDone', live: 'L', result: 'R' },
  previewFailed: { type: 'previewFailed', code: 'unprocessable', message: 'denied' },
  previewUnsupported: { type: 'previewFailed', code: 'dry_run_unsupported', message: 'no' },
  saveStart: { type: 'saveStart' },
  saveDone: { type: 'saveDone' },
  saveFailed: { type: 'saveFailed', code: 'internal', message: 'boom' },
  saveConflict: { type: 'saveFailed', code: 'conflict', message: 'stale' },
};

const phases: SavePhase[] = ['idle', 'previewing', 'previewed', 'saving', 'saved', 'conflict', 'error'];

// from phase × event → expected phase. Anything not listed keeps the phase.
const transitions: Record<string, Partial<Record<keyof typeof events, SavePhase>>> = {
  idle: { previewStart: 'previewing', saveStart: 'saving' },
  previewing: { edit: 'idle', previewDone: 'previewed', previewFailed: 'error', previewUnsupported: 'error' },
  previewed: { edit: 'idle', previewStart: 'previewing', saveStart: 'saving' },
  saving: { edit: 'idle', saveDone: 'saved', saveFailed: 'error', saveConflict: 'conflict' },
  saved: { edit: 'idle', previewStart: 'previewing', saveStart: 'saving' },
  conflict: { edit: 'idle', previewStart: 'previewing', saveStart: 'saving' },
  error: { edit: 'idle', previewStart: 'previewing', saveStart: 'saving' },
};

describe('saveReducer', () => {
  for (const phase of phases) {
    for (const [name, event] of Object.entries(events)) {
      const want = transitions[phase]?.[name] ?? phase;
      it(`${phase} + ${name} → ${want}`, () => {
        expect(saveReducer(at(phase), event).phase).toBe(want);
      });
    }
  }

  it('keeps the dry-run result for the diff', () => {
    let s = saveReducer(initialSaveState, events.previewStart as SaveEvent);
    s = saveReducer(s, events.previewDone as SaveEvent);
    expect(s.preview).toEqual({ live: 'L', result: 'R' });
    expect(saveReducer(s, events.edit as SaveEvent)).toEqual(initialSaveState);
  });

  it('remembers that this kind cannot be previewed, and keeps remembering across a failed save', () => {
    let s = saveReducer(at('previewing'), events.previewUnsupported as SaveEvent);
    expect(s).toMatchObject({
      phase: 'error',
      previewUnsupported: true,
      error: { code: 'dry_run_unsupported' },
    });
    s = saveReducer(s, events.saveStart as SaveEvent);
    s = saveReducer(s, events.saveFailed as SaveEvent);
    expect(s).toMatchObject({ phase: 'error', previewUnsupported: true, error: { message: 'boom' } });
  });

  it('records the error and clears it when a save starts', () => {
    const failed = saveReducer(at('saving'), events.saveFailed as SaveEvent);
    expect(failed.error).toEqual({ code: 'internal', message: 'boom' });
    expect(saveReducer(failed, events.saveStart as SaveEvent).error).toBeUndefined();
  });

  it('editing an untouched editor changes nothing', () => {
    const s = at('idle');
    expect(saveReducer(s, events.edit as SaveEvent)).toBe(s);
  });
});
