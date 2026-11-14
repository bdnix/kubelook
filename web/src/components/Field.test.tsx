import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { Button } from './Button';
import { ErrorText } from './ErrorText';
import { Field } from './Field';

describe('Field', () => {
  it('labels its input and passes the type', async () => {
    const user = userEvent.setup();
    render(<Field label="Vault token" type="password" />);
    const input = screen.getByLabelText('Vault token');
    expect(input).toHaveAttribute('type', 'password');
    await user.type(input, 'abc');
    expect(input).toHaveValue('abc');
  });

  it('renders a textarea when asked', () => {
    render(<Field label="Paste it" multiline />);
    expect(screen.getByLabelText('Paste it').tagName).toBe('TEXTAREA');
  });

  it('shows hint text associated with the input', () => {
    render(<Field label="Label" hint="Shown in the rail" />);
    expect(screen.getByLabelText('Label')).toHaveAccessibleDescription('Shown in the rail');
  });
});

describe('Button', () => {
  it('is a button that does not submit unless told to', () => {
    render(<Button onClick={() => undefined}>Go</Button>);
    expect(screen.getByRole('button', { name: 'Go' })).toHaveAttribute('type', 'button');
  });

  it('can be a submit button and can be disabled', () => {
    render(
      <Button type="submit" disabled>
        Send
      </Button>,
    );
    const button = screen.getByRole('button', { name: 'Send' });
    expect(button).toHaveAttribute('type', 'submit');
    expect(button).toBeDisabled();
  });
});

describe('ErrorText', () => {
  it('renders nothing without a message', () => {
    const { container } = render(<ErrorText message={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('announces the message as plain text', () => {
    render(<ErrorText message="<b>nope</b>" />);
    expect(screen.getByRole('alert')).toHaveTextContent('<b>nope</b>');
  });
});
