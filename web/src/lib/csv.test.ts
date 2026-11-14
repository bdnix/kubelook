import { describe, expect, it } from 'vitest';
import { BOM, neutralize, toCsv } from './csv';

describe('toCsv', () => {
  it('starts with a UTF-8 byte order mark and ends every line, the last too, with CRLF', () => {
    expect(toCsv(['a', 'b'], [['1', '2']])).toBe(`${BOM}a,b\r\n1,2\r\n`);
    expect(BOM).toBe('﻿');
    expect(toCsv(['a'], [])).toBe(`${BOM}a\r\n`);
  });

  it('quotes fields with a comma, a quote or a line break, and doubles the quotes', () => {
    expect(toCsv(['x'], [['a,b'], ['say "hi"'], ['two\nlines'], ['cr\rhere'], ['plain']])).toBe(
      `${BOM}x\r\n"a,b"\r\n"say ""hi"""\r\n"two\nlines"\r\n"cr\rhere"\r\nplain\r\n`,
    );
  });

  it('writes numbers as text and missing values as nothing', () => {
    expect(toCsv(['n', 'm', 'o'], [[3, undefined as unknown as string, null as unknown as string]])).toBe(
      `${BOM}n,m,o\r\n3,,\r\n`,
    );
    expect(toCsv(['n'], [[0], [1.5]])).toBe(`${BOM}n\r\n0\r\n1.5\r\n`);
  });

  it('keeps non-ASCII text as it is', () => {
    expect(toCsv(['x'], [['Zoë ✓']])).toBe(`${BOM}x\r\nZoë ✓\r\n`);
  });

  it('neutralises formulas in headers and cells before quoting', () => {
    expect(toCsv(['=h'], [['=1+1,2'], ['+1'], ['-1'], ['@SUM(A1)'], ['ok']])).toBe(
      `${BOM}'=h\r\n"'=1+1,2"\r\n'+1\r\n'-1\r\n'@SUM(A1)\r\nok\r\n`,
    );
  });
});

describe('neutralize', () => {
  it.each(['=cmd', '+1', '-1', '@x', ' =x', '  +x', '\t=x', '\r=x', '\n@x', ' \t\r -x'])(
    'prefixes %j with a single quote',
    (cell) => {
      expect(neutralize(cell)).toBe(`'${cell}`);
    },
  );

  it.each(['', 'a=b', '1-2', 'x+y', "'=x", 'name@host', ' ', 'plain text'])('leaves %j alone', (cell) => {
    expect(neutralize(cell)).toBe(cell);
  });
});
