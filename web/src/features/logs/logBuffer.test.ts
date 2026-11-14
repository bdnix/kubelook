import { describe, expect, it } from 'vitest';
import { appendLines, markDropped, type LogLine } from './logBuffer';

const line = (n: number): LogLine => ({ ts: `t${n}`, text: `line ${n}` });

describe('logBuffer', () => {
  it('keeps at most max lines and drops the oldest', () => {
    let buf: LogLine[] = [];
    buf = appendLines(buf, [line(1), line(2), line(3)], 5);
    buf = appendLines(buf, [line(4), line(5), line(6), line(7)], 5);
    expect(buf.map((l) => l.text)).toEqual(['line 3', 'line 4', 'line 5', 'line 6', 'line 7']);
  });

  it('does not change the input array', () => {
    const buf = [line(1)];
    appendLines(buf, [line(2)], 5);
    expect(buf).toHaveLength(1);
  });

  it('inserts a visible marker for dropped lines', () => {
    const buf = markDropped([line(1)], 2000, 10);
    expect(buf[1]).toEqual({ ts: '', text: '… 2000 lines dropped …', marker: true });
    expect(markDropped([], 1, 10)[0]?.text).toBe('… 1 line dropped …');
  });

  it('the marker counts against the limit', () => {
    const buf = markDropped([line(1), line(2)], 5, 2);
    expect(buf.map((l) => l.text)).toEqual(['line 2', '… 5 lines dropped …']);
  });
});
