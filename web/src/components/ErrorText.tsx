/** An error message as plain text, announced to assistive technology. Renders nothing without a message. */
export function ErrorText({ message }: { message: string | undefined }) {
  if (!message) return null;
  return (
    <p className="error-text" role="alert">
      {message}
    </p>
  );
}
