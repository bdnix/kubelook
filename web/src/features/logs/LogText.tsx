import { SafeLink } from '../../components/SafeLink';
import { highlight } from '../../lib/highlight';
import { tokenize } from './tokens';

export interface LogTextProps {
  text: string;
  /** The reader's search, marked inside the text. */
  highlight?: { query: string; regex: boolean };
}

/** Log text as React text children: timestamps, UUIDs and loggers get a class, URLs become safe links. */
export function LogText({ text, highlight: search }: LogTextProps) {
  return (
    <>
      {tokenize(text).map((token, i) => {
        const segments = search
          ? highlight(token.text, search.query, search.regex).segments
          : [{ text: token.text, match: false }];
        const body = segments.map((s, j) =>
          s.match ? <mark key={j}>{s.text}</mark> : <span key={j}>{s.text}</span>,
        );
        if (token.type === 'url') {
          return (
            <SafeLink key={i} href={token.text}>
              <span className="tok-url">{body}</span>
            </SafeLink>
          );
        }
        return (
          <span key={i} className={token.type === 'text' ? undefined : `tok-${token.type}`}>
            {body}
          </span>
        );
      })}
    </>
  );
}
