const MESSAGES: ReadonlyMap<string, string> = new Map([
  ['not_allowed', "This account isn't allowed to sign in."],
  ['state', 'The sign-in attempt expired or could not be verified. Please try again.'],
  ['denied', 'Sign-in was cancelled.'],
  ['failed', 'Sign-in failed. Please try again.'],
]);

/** The fixed text for a `signin_error` code. Unknown codes give nothing: they are never echoed. */
export function signinErrorMessage(code: string | null): string | undefined {
  return code === null ? undefined : MESSAGES.get(code);
}
