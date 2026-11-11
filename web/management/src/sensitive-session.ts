// RAM-only generation shared by every API scope and sensitive page.
let generation = 0;
const listeners = new Set<() => void>();
export function sessionGeneration() { return generation; }
export function invalidateSensitive() {
  generation++;
  for (const listener of [...listeners]) listener();
}
export function onSensitiveInvalidated(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', invalidateSensitive);
  window.addEventListener('management-session-expired', invalidateSensitive);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState !== 'visible') invalidateSensitive();
  });
}
