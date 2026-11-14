import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';
import { ErrorText } from '../../components/ErrorText';
import type { K8sObject } from '../../state/live';
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

/** The YAML tab: edit, preview with a dry run, save with a full PUT. */
export function EditorTab({ target, kind, object, canEdit }: EditorTabProps) {
  const s = useEditorState(target, object);
  const schema = useSchema(target.cluster, target.group, target.version, kind, canEdit);
  const { phase, error, previewUnsupported } = s.save;
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
