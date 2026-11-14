import { ApiError } from '../../api/client';

/** The text to show for a failed request: the server's own message when it sent one. */
export function errorText(error: unknown): string {
  return error instanceof ApiError ? error.message : 'Something went wrong. Please try again.';
}
