// App and page are separate native bundles: callbacks live on the native App
// instance, rather than each bundle's private copy of this module.
export function setCleanup(fn: (() => void) | undefined) {
  getApp().onboardingCleanup = fn;
}
export function setLaunch(fn: ((options: any) => void) | undefined) {
  getApp().onboardingLaunch = fn;
}
export function hide(app: any) {
  app.onboardingCleanup?.();
}
export function show(app: any, options: any) {
  app.onboardingLaunch?.(options);
}
