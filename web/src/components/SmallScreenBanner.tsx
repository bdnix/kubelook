import { useEffect, useState } from 'react';

const QUERY = '(min-width: 1024px)';

/** Asks for a desktop browser when the window is narrower than 1024 px. */
export function SmallScreenBanner({
  matchMedia = typeof window !== 'undefined' ? window.matchMedia?.bind(window) : undefined,
}: {
  matchMedia?: (query: string) => MediaQueryList;
}) {
  const [narrow, setNarrow] = useState(() => (matchMedia ? !matchMedia(QUERY).matches : false));
  useEffect(() => {
    if (!matchMedia) return undefined;
    const mql = matchMedia(QUERY);
    const update = () => setNarrow(!mql.matches);
    mql.addEventListener('change', update);
    return () => mql.removeEventListener('change', update);
  }, [matchMedia]);
  if (!narrow) return null;
  return (
    <p role="status" className="small-screen">
      Kubelook is designed for desktop browsers.
    </p>
  );
}
