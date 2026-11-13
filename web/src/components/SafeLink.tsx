import type { ReactNode } from 'react';

function isWebUrl(href: string): boolean {
  try {
    const { protocol, host } = new URL(href);
    return (protocol === 'http:' || protocol === 'https:') && host !== '';
  } catch {
    return false;
  }
}

/** The only way to turn a cluster-supplied URL into a link: http(s) only, otherwise plain text. */
export function SafeLink({ href, children }: { href: string; children: ReactNode }) {
  if (!isWebUrl(href)) return <span>{children}</span>;
  return (
    <a href={href} rel="noopener noreferrer" target="_blank">
      {children}
    </a>
  );
}
