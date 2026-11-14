const BY_EXTENSION: Record<string, string> = {
  json: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  md: 'markdown',
  sh: 'shell',
  bash: 'shell',
  js: 'javascript',
  mjs: 'javascript',
  ts: 'typescript',
  py: 'python',
  go: 'go',
  rs: 'rust',
  java: 'java',
  xml: 'xml',
  html: 'html',
  css: 'css',
  sql: 'sql',
  ini: 'ini',
  toml: 'ini',
  conf: 'ini',
  log: 'plaintext',
  txt: 'plaintext',
};

const BY_NAME: Record<string, string> = { dockerfile: 'dockerfile', makefile: 'makefile' };

/** The editor language for a file path, chosen from its extension or well-known name. */
export function languageFor(path: string): string {
  const base = path.slice(path.lastIndexOf('/') + 1).toLowerCase();
  const named = BY_NAME[base];
  if (named) return named;
  const dot = base.lastIndexOf('.');
  return (dot > 0 ? BY_EXTENSION[base.slice(dot + 1)] : undefined) ?? 'plaintext';
}
