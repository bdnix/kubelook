import { useCallback, useMemo, useReducer, useState } from 'react';
import { ApiError } from '../../api/client';
import { useApi } from '../../api/context';
import { diffLines, type DiffLine } from '../../lib/diff';
import { getString } from '../../lib/path';
import { parseObject, toYaml } from '../../lib/yaml';
import type { K8sObject } from '../../state/live';
import { initialSaveState, saveReducer, type SaveState } from './saveMachine';

export interface EditTarget {
  cluster: string;
  group: string;
  version: string;
  resource: string;
  /** Empty for cluster-scoped kinds. */
  namespace: string;
  name: string;
}

export const IDENTITY_MESSAGE =
  "The name, namespace, apiVersion and kind of an object can't be changed. Create a new object instead.";

function pathOf(t: EditTarget): string {
  const ns = t.namespace ? `?namespace=${encodeURIComponent(t.namespace)}` : '';
  return `/clusters/${encodeURIComponent(t.cluster)}/resources/${t.group === '' ? 'core' : t.group}/${t.version}/${t.resource}/${encodeURIComponent(t.name)}${ns}`;
}

function failure(e: unknown): { code: string; message: string } {
  if (e instanceof ApiError) return { code: e.code, message: e.message };
  return { code: 'internal', message: 'Something went wrong.' };
}

function identityOf(o: K8sObject): string[] {
  return [
    getString(o, 'apiVersion'),
    getString(o, 'kind'),
    getString(o, 'metadata.name'),
    getString(o, 'metadata.namespace'),
  ];
}

/**
 * The text of the editor and the save flow of one object. `object` is the live object: while the editor is
 * clean its text follows it; once the user has edited, a newer version is offered, never forced on them.
 */
export function useEditorState(target: EditTarget, object: K8sObject) {
  const api = useApi();
  const latestText = useMemo(() => toYaml(object), [object]);
  const latestVersion = getString(object, 'metadata.resourceVersion');
  // `latest` is the live version when the base was set: a stale live object after our own save is not news.
  const [base, setBase] = useState({ text: latestText, version: latestVersion, latest: latestVersion });
  const [text, setText] = useState(latestText);
  const [save, dispatch] = useReducer(saveReducer, initialSaveState);
  const [showDiff, setShowDiff] = useState(false);

  const dirty = text !== base.text;
  // Clean and behind: take the newer version silently. (Set during render: React re-renders at once.)
  const newer = latestVersion !== base.latest && latestVersion !== base.version;
  if (!dirty && newer) {
    setBase({ text: latestText, version: latestVersion, latest: latestVersion });
    setText(latestText);
  }
  const remoteChanged = dirty && newer;

  const parsed = useMemo(() => parseObject(text), [text]);
  const parseError = parsed.ok ? undefined : parsed.error;
  const identityError =
    parsed.ok && identityOf(parsed.value).join('\u0000') !== identityOf(object).join('\u0000')
      ? IDENTITY_MESSAGE
      : undefined;
  const valid = parsed.ok && identityError === undefined;

  const edit = useCallback((next: string) => {
    setText(next);
    dispatch({ type: 'edit' });
  }, []);

  const reload = useCallback(() => {
    setBase({ text: latestText, version: latestVersion, latest: latestVersion });
    setText(latestText);
    setShowDiff(false);
    dispatch({ type: 'edit' });
  }, [latestText, latestVersion]);

  const preview = useCallback(async () => {
    if (!parsed.ok) return;
    dispatch({ type: 'previewStart' });
    try {
      const r = await api.put<{ live: K8sObject; result: K8sObject }>(
        `${pathOf(target)}${target.namespace ? '&' : '?'}dryRun=true`,
        parsed.value,
      );
      dispatch({ type: 'previewDone', live: r.live, result: r.result });
    } catch (e) {
      dispatch({ type: 'previewFailed', ...failure(e) });
    }
  }, [api, parsed, target]);

  const commit = useCallback(async () => {
    if (!parsed.ok) return;
    dispatch({ type: 'saveStart' });
    try {
      const result = await api.put<K8sObject>(pathOf(target), parsed.value);
      const savedText = toYaml(result);
      setBase({
        text: savedText,
        version: getString(result, 'metadata.resourceVersion'),
        latest: latestVersion,
      });
      setText(savedText);
      dispatch({ type: 'saveDone' });
    } catch (e) {
      dispatch({ type: 'saveFailed', ...failure(e) });
    }
  }, [api, parsed, target, latestVersion]);

  const previewDiff: DiffLine[] | undefined = useMemo(() => {
    const p = (save as SaveState).preview;
    if (!p) return undefined;
    return diffLines(toYaml(p.live as K8sObject), toYaml(p.result as K8sObject));
  }, [save]);

  return {
    text,
    edit,
    dirty,
    valid,
    parseError,
    identityError,
    remoteChanged,
    /** The live object as text, to diff the editor against. */
    latestText,
    showDiff,
    setShowDiff,
    reload,
    save,
    preview,
    commit,
    previewDiff,
  };
}
