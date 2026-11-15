/** The UTF-8 byte order mark that makes spreadsheets read the file as UTF-8. */
export const BOM = '﻿';

/**
 * Stops a spreadsheet from running a cell as a formula: a cell that starts with =, +, - or @ (also after
 * white space, a tab or a carriage return) gets a single quote in front.
 */
export function neutralize(cell: string): string {
  return /^\s*[=+\-@]/.test(cell) ? `'${cell}` : cell;
}

function field(value: string | number | null | undefined): string {
  // neutralise the text first, then quote: the quote character must not hide the trigger from a spreadsheet
  const text = neutralize(value === null || value === undefined ? '' : String(value));
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

/** RFC 4180: CRLF line ends (also after the last line), quotes doubled, a byte order mark in front. */
export function toCsv(headers: string[], rows: Array<Array<string | number>>): string {
  const lines = [headers, ...rows].map((r) => r.map(field).join(','));
  return BOM + lines.map((l) => `${l}\r\n`).join('');
}
