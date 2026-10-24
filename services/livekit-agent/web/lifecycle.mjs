export function aborted() { return new DOMException('操作已取消', 'AbortError'); }

export class AsyncResource {
  constructor() { this.generation = 0; this.pending = null; this.value = null; }
  acquire(factory, dispose) {
    if (this.value) return Promise.resolve(this.value);
    if (this.pending) return this.pending;
    const generation = this.generation;
    const pending = Promise.resolve().then(factory).then(value => {
      if (generation !== this.generation) { dispose(value); throw aborted(); }
      this.value = value; this.dispose = dispose;
      return value;
    }).finally(() => { if (this.pending === pending) this.pending = null; });
    this.pending = pending;
    return pending;
  }
  clear() {
    this.generation++; this.pending = null;
    if (this.value) this.dispose(this.value);
    this.value = null;
  }
}

export class AttemptGate {
  constructor() { this.active = null; }
  get busy() { return this.active !== null; }
  begin() { if (this.active) return null; return this.active = {}; }
  current(token) { return this.active === token; }
  finish(token) { if (this.current(token)) this.active = null; }
  cancel() { this.active = null; }
}
