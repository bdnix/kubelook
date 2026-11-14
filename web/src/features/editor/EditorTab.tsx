import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import { bytesToBase64 } from '../../lib/bytes';
import { parseObject, stringifyObject } from '../../lib/yaml';
import type { K8sObject } from '../../state/live';
import { useReveal } from '../secrets/useReveal';
import { ConflictBanner } from './ConflictBanner';
import { kubectlCommand } from './kubectlCommand';
import { DiffViewer, YamlEditor } from './lazyEditors';
import { SavePreview } from './SavePreview';
import { useEditorState, type EditTarget } from './useEditorState';
import { useSchema } from './useSchema';

export interface EditorTabProps {
  target: EditTarget;
  kind: string;
  object: K8sObject;
  /** Whether the user may update the object; without it the editor is read only. */
  canEdit: boolean;
}

function maskedKeys(text: string, kind: string): string[] {
  if (kind !== 'Secret') return [];
  const parsed = parseObject(text);
  const data = parsed.ok ? (parsed.value.data as Record<string, unknown> | undefined) : undefined;
  return Object.entries(data ?? {})
    .filter(([, v]) => v === '********')
    .map(([k]) => k)
    .sort();
}

/** The YAML tab: edit, preview with a dry run, save with a full PUT. */
export function EditorTab({ target, kind, object, canEdit }: EditorTabProps) {
  const s = useEditorState(target, object);
  const schema = useSchema(target.cluster, target.group, target.version, kind, canEdit);
  const { phase, error, previewUnsupported } = s.save;
  const reveal = useReveal(target.cluster, target.namespace, target.name);
  const masked = maskedKeys(s.text, kind);
  const revealForEditing = async (key: string) => {
    const got = await reveal.reveal([key]);
    const value = got?.[key];
    const parsed = parseObject(s.text);
    if (value === undefined || !parsed.ok) return;
    const data = {
      ...((parsed.value.data as Record<string, unknown> | undefined) ?? {}),
      [key]: bytesToBase64(value),
    };
    s.edit(stringifyObject({ ...parsed.value, data }));
  };
  const busy = phase === 'previewing' || phase === 'saving';
  const ready = canEdit && s.dirty && s.valid && !busy;
  const showError = phase === 'error' && error !== undefined && !previewUnsupported;

  return (
    <div>
      {phase === 'conflict' ? (
        <ConflictBanner kind="save" onReload={s.reload} onDiff={() => s.setShowDiff(true)} />
      ) : (
        s.remoteChanged && (
          <ConflictBanner kind="remote" onReload={s.reload} onDiff={() => s.setShowDiff(true)} />
        )
      )}
      {!canEdit && <Banner kind="info">You can read this object but not change it.</Banner>}
      {s.showDiff ? (
        <>
          <DiffViewer original={s.latestText} modified={s.text} label={`Live ${kind} against your changes`} />
          <Button onClick={() => s.setShowDiff(false)}>Back to the editor</Button>
        </>
      ) : (
        <YamlEditor
          label={`${kind} YAML`}
          value={s.text}
          onChange={s.edit}
          readOnly={!canEdit}
          schema={schema?.schema}
          schemaRef={schema?.schemaRef}
        />
      )}
      {canEdit && masked.length > 0 && (
        <section aria-label="Secret values">
          <p>
            Secret values show as ******** and are kept as they are. Reveal a key to change it (each reveal is
            audited).
          </p>
          {masked.map((k) => (
            <Button
              key={k}
              onClick={() => void revealForEditing(k)}
              disabled={reveal.busy}
            >{`Reveal ${k} for editing`}</Button>
          ))}
          <ErrorText message={reveal.error} />
        </section>
      )}
      <ErrorText message={s.parseError} />
      <ErrorText message={s.identityError} />
      {showError && <ErrorText message={error.message} />}
      {previewUnsupported && <p role="status">A preview isn&apos;t available for this object.</p>}
      {phase === 'saved' && <p role="status">Saved.</p>}
      {s.previewDiff && <SavePreview diff={s.previewDiff} />}
      <div>
        <Button onClick={() => void s.preview()} disabled={!ready || previewUnsupported}>
          Preview
        </Button>
        <Button onClick={() => void s.commit()} disabled={!ready}>
          {previewUnsupported ? 'Save without preview' : 'Save'}
        </Button>
        <Button onClick={s.reload} disabled={!s.dirty}>
          Discard changes
        </Button>
      </div>
      <p>
        Equivalent: <code>{kubectlCommand({ type: 'replace', namespace: target.namespace })}</code>
      </p>
    </div>
  );
}
