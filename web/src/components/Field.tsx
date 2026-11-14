import { useId, type InputHTMLAttributes, type TextareaHTMLAttributes } from 'react';

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'id'>;
type AreaProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id'>;

export type FieldProps = (InputProps & { multiline?: false }) | (AreaProps & { multiline: true });

/** A labelled input or textarea with optional hint text. */
export function Field({ label, hint, ...rest }: FieldProps & { label: string; hint?: string }) {
  const id = useId();
  const hintId = `${id}-hint`;
  const describedBy = hint ? hintId : undefined;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {rest.multiline ? (
        <textarea {...omitMultiline(rest)} id={id} aria-describedby={describedBy} />
      ) : (
        <input {...omitMultiline(rest)} id={id} aria-describedby={describedBy} />
      )}
      {hint && <small id={hintId}>{hint}</small>}
    </div>
  );
}

function omitMultiline<T extends { multiline?: boolean }>(props: T): Omit<T, 'multiline'> {
  const { multiline: _multiline, ...rest } = props;
  return rest;
}
