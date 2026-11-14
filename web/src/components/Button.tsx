import type { ButtonHTMLAttributes } from 'react';

/** A button that never submits a form unless asked to. */
export function Button({ type = 'button', ...rest }: ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type={type} {...rest} />;
}
