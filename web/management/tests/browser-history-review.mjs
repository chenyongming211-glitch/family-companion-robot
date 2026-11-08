// Deterministic persisted-pageshow fixture; this does not reproduce a natural bfcache navigation.
// Run on the Mac browser client with ego-browser nodejs < tests/browser-history-review.mjs.
const task = await taskSpace('伴伴管理平台前端评审');
const page = task.page('p1');
await page.goto(`http://127.0.0.1:18790/index.html?historyfix=${Date.now()}#/configuration?device=dev-living`);
await page.waitForSelector('#name');
await page.click('loc=role:button[name="重置演示"]');
await page.acceptDialog();
await page.waitForFunction(() => document.querySelector('.reset-message')?.textContent.includes('已重置'));
await page.evaluate(() => {
  const key = 'companion-management-demo-v1';
  const snapshot = JSON.parse(sessionStorage.getItem(key));
  snapshot.data.conversations = snapshot.data.conversations.filter(c => c.id !== 'chat-story');
  snapshot.data.memories = snapshot.data.memories.filter(m => m.conversationId !== 'chat-story');
  snapshot.data.devices.find(d => d.id === 'dev-living').name = '家庭新名称';
  sessionStorage.setItem(key, JSON.stringify(snapshot));
  window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true }));
});
let reloaded = true;
try {
  await page.waitForFunction(() => document.querySelector('#name')?.value === '家庭新名称', undefined, { timeout: 3000 });
} catch {
  try {
    await page.waitForSelector('#name');
    await page.waitForFunction(() => document.querySelector('#name')?.value === '家庭新名称', undefined, { timeout: 3000 });
  } catch { reloaded = false; }
}
await page.fill('#name', '保存后名称');
await page.click('loc=role:button[name="保存设置"]');
await page.waitForFunction(() => document.querySelector('form [role="status"]')?.textContent.includes('已保存'));
const result = await page.evaluate(() => {
  const data = JSON.parse(sessionStorage.getItem('companion-management-demo-v1')).data;
  return { chatPresent: data.conversations.some(c => c.id === 'chat-story'), memoryPresent: data.memories.some(m => m.conversationId === 'chat-story'), name: data.devices.find(d => d.id === 'dev-living').name };
});
const pass = reloaded && !result.chatPresent && !result.memoryPresent && result.name === '保存后名称';
console.log(JSON.stringify({ fixture: 'persisted-pageshow', reloaded, ...result, pass }));
await page.click('loc=role:button[name="重置演示"]');
await page.acceptDialog();
await page.waitForFunction(() => document.querySelector('.reset-message')?.textContent.includes('已重置'));
if (!pass) throw new Error('Persisted-pageshow regression failed');
