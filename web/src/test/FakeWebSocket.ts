/** A WebSocket the tests drive by hand. Every instance is kept in `FakeWebSocket.instances`. */
export class FakeWebSocket {
  static instances: FakeWebSocket[] = [];
  static reset(): void {
    FakeWebSocket.instances = [];
  }
  static get last(): FakeWebSocket {
    const ws = FakeWebSocket.instances[FakeWebSocket.instances.length - 1];
    if (!ws) throw new Error('no WebSocket was created');
    return ws;
  }

  static readonly OPEN = 1;

  readonly url: string;
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    FakeWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sent.push(data);
  }

  close(code = 1000): void {
    this.readyState = 3;
    this.onclose?.({ code });
  }

  /** The frames the client sent, parsed. */
  get frames(): Array<{ op: string; ch: number; d?: unknown }> {
    return this.sent.map((s) => JSON.parse(s) as { op: string; ch: number; d?: unknown });
  }

  // --- the server's side ---

  serverOpen(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.onopen?.();
  }

  serverSend(frame: { ch: number; t: string; d?: unknown }): void {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  serverClose(code: number): void {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}
