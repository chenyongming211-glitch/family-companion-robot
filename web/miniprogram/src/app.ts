import { hide, show } from "./lifecycle.ts";
App({
  onShow(options: any) {
    show(options);
  },
  onHide() {
    hide();
  },
});
