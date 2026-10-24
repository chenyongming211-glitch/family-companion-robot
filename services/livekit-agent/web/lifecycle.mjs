export function aborted() { return new DOMException('操作已取消', 'AbortError'); }

export function serviceErrorMessage(event) {
  if (event.code === 'session_expired') return '本次测试会话已到期，请重新连接；配对过期时需重新配对。上一条问题未自动重发。';
  if (event.code === 'turn_limit') return '本次测试已达到轮数上限，请断开并重新连接。';
  if (event.code === 'operation_timeout') return `服务操作超时（${event.stage || 'unknown'}），请重新连接；上一条问题未自动重发。`;
  return `服务错误：${event.code || event.stage || 'unknown'}（${event.error_type || 'unknown'}），本次不会自动重试`;
}

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
