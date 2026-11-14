import { describe, expect, it } from 'vitest';
import { languageFor } from './language';

describe('languageFor', () => {
  it.each([
    ['/etc/app/config.JSON', 'json'],
    ['/srv/a.yml', 'yaml'],
    ['/srv/a.yaml', 'yaml'],
    ['/usr/bin/run.sh', 'shell'],
    ['/app/main.go', 'go'],
    ['/app/Dockerfile', 'dockerfile'],
    ['/app/Makefile', 'makefile'],
    ['/etc/hosts', 'plaintext'],
    ['/etc/.bashrc', 'plaintext'],
    ['/etc/file.unknownext', 'plaintext'],
    ['/var/log/app.log', 'plaintext'],
    ['relative.py', 'python'],
  ])('%s → %s', (path, language) => expect(languageFor(path)).toBe(language));
});
