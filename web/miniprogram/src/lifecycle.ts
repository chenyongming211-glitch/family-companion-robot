// App and page are separate native bundles: callbacks live on the native App
// instance, rather than each bundle's private copy of this module.
export function setCleanup(fn: (() => void) | undefined) {
  getApp().onboardingCleanup = fn;
}
export function setLaunch(fn: ((options: any) => void) | undefined) {
  getApp().onboardingLaunch = fn;
}
export function hide() {
  getApp().onboardingCleanup?.();
}
export function show(options: any) {
  getApp().onboardingLaunch?.(options);
}
