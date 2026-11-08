// Run with ego-browser nodejs < tests/browser-review.mjs on the Mac browser client.
// The static bundle and all runtime checks run on the cloud server.
const review = await taskSpace("伴伴管理平台前端评审");
const page = review.page("p1");
const base = "http://127.0.0.1:18790/";
const admin = [
  ["overview", "管理总览"], ["families", "家庭管理"], ["family", "林的家"],
  ["devices", "设备管理"], ["device", "团团"], ["conversations", "聊天记录"],
  ["daily", "每日概览"], ["memories", "设备记忆"], ["capabilities", "能力管理"],
  ["expressions", "表情预览"], ["voiceprints", "声纹验收"], ["tasks", "任务与编排"],
  ["configuration", "设备配置"], ["policies", "权限策略"], ["firmware", "固件能力"],
  ["operations", "运行概览"], ["audit", "审计记录"], ["accounts", "账号管理"], ["login", "内部登录"],
];
const family = [
  ["home", "和家人一起，慢慢长大"], ["device", "团团"], ["conversations", "聊天记录"],
  ["conversation", "小狐狸和月亮的故事"], ["memories", "设备记忆"], ["memory", "记忆详情"],
  ["family", "林的家"], ["invite", "邀请家庭成员"], ["bind", "绑定设备"],
  ["settings", "团团的设置"], ["privacy", "隐私设置"], ["login", "家庭登录"],
];
await page.goto(`${base}index.html?review=full#/overview`);
await page.waitForSelector("main h1");
await page.click('loc=role:button[name="重置演示"]');
await page.acceptDialog();
await page.waitForFunction(() => document.querySelector('[role="status"]')?.textContent.includes("已重置"));
const results = [];
for (const width of [1440, 768, 390]) {
  await page.cdp("Emulation.setDeviceMetricsOverride", { width, height: 900, deviceScaleFactor: 1, mobile: width < 768 });
  for (const [entry, routes] of [["index.html", admin], ["family.html", family]]) {
    for (const [route, expected] of routes) {
      const query = `device=dev-living&chat=chat-story&family=family-lin${route === "memory" ? "&memory=memory-story" : ""}`;
      try {
        await page.goto(`${base}${entry}?review=full#/${route}?${query}`);
        await page.waitForFunction(title => document.querySelector("main h1")?.textContent === title, expected, { timeout: 5000 });
        const layout = await page.evaluate(() => {
          const ids = [...document.querySelectorAll("[id]")].map(n => n.id);
          return {
            width: innerWidth, scroll: document.documentElement.scrollWidth,
            duplicateIds: ids.filter((id, i) => ids.indexOf(id) !== i),
            demoLabel: document.body.textContent.includes("前端演示 · 虚构数据"),
          };
        });
        results.push({ entry, route, viewport: width, ...layout, pass: layout.scroll <= layout.width + 1 && !layout.duplicateIds.length && layout.demoLabel });
      } catch (error) { results.push({ entry, route, viewport: width, pass: false, error: String(error) }); }
    }
  }
}
const fs = await import("node:fs/promises");
const os = await import("node:os");
const path = await import("node:path");
const output = path.join(os.tmpdir(), "companion-management-routes-review.json");
await fs.writeFile(output, JSON.stringify(results, null, 2));
const failures = results.filter(item => !item.pass);
console.log({ output, spaceId: review.spaceId, checked: results.length, passed: results.length - failures.length, failures });
if (failures.length) throw new Error("Route or responsive review failed; see routes-review.json");
