import { Banner } from '../../components/Banner';
import { Button } from '../../components/Button';

export type ConflictKind = 'remote' | 'save';

/**
 * Someone else changed the object. `remote`: the live object moved on while the user edited; `save`: the save
 * was refused because of it. Either way the user chooses between taking the new version and comparing.
 */
export function ConflictBanner({
  kind,
  onReload,
  onDiff,
}: {
  kind: ConflictKind;
  onReload: () => void;
  onDiff: () => void;
}) {
  return (
    <Banner kind="warning">
      {kind === 'save'
        ? 'The object changed since you loaded it, so your changes were not saved.'
        : 'The object changed in the cluster while you were editing.'}{' '}
      <Button onClick={onReload}>Reload</Button> <Button onClick={onDiff}>Diff against mine</Button>
    </Banner>
  );
}
