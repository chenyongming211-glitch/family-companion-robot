import { hide, show } from "./lifecycle.ts";
App({
  onShow(options: any) {
    show(this, options);
  },
  onHide() {
    hide(this);
  },
});
