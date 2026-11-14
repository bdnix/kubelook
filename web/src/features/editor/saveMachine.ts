export type SavePhase = 'idle' | 'previewing' | 'previewed' | 'saving' | 'saved' | 'conflict' | 'error';

export interface SaveState {
  phase: SavePhase;
  /** The dry-run result, as text of `live` and `result`, once previewed. */
  preview?: { live: unknown; result: unknown };
  error?: { code: string; message: string };
  /** The server cannot preview this kind: offer "Save without preview". */
  previewUnsupported: boolean;
}

export type SaveEvent =
  | { type: 'edit' }
  | { type: 'previewStart' }
  | { type: 'previewDone'; live: unknown; result: unknown }
  | { type: 'previewFailed'; code: string; message: string }
  | { type: 'saveStart' }
  | { type: 'saveDone' }
  | { type: 'saveFailed'; code: string; message: string };

export const initialSaveState: SaveState = { phase: 'idle', previewUnsupported: false };

/**
 * idle → previewing → previewed → saving → saved | conflict | error. Editing starts over. Events that do not
 * fit the current phase are ignored (a late answer after the user edited again must not change anything).
 */
export function saveReducer(state: SaveState, event: SaveEvent): SaveState {
  switch (event.type) {
    case 'edit':
      return state.phase === 'idle' && !state.previewUnsupported ? state : initialSaveState;
    case 'previewStart':
      return state.phase === 'saving' || state.phase === 'previewing'
        ? state
        : { ...initialSaveState, phase: 'previewing' };
    case 'previewDone':
      return state.phase === 'previewing'
        ? {
            phase: 'previewed',
            preview: { live: event.live, result: event.result },
            previewUnsupported: false,
          }
        : state;
    case 'previewFailed':
      if (state.phase !== 'previewing') return state;
      return {
        phase: 'error',
        error: { code: event.code, message: event.message },
        previewUnsupported: event.code === 'dry_run_unsupported',
      };
    case 'saveStart':
      return state.phase === 'saving' || state.phase === 'previewing'
        ? state
        : { ...state, phase: 'saving', error: undefined };
    case 'saveDone':
      return state.phase === 'saving' ? { phase: 'saved', previewUnsupported: false } : state;
    case 'saveFailed':
      if (state.phase !== 'saving') return state;
      return {
        phase: event.code === 'conflict' ? 'conflict' : 'error',
        error: { code: event.code, message: event.message },
        previewUnsupported: state.previewUnsupported,
      };
  }
}
