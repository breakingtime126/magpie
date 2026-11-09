// Original Agents page and editor, with API fixtures only. No native programs,
// accounts, provider persistence, gateway tests or agent configuration writes.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");
const words = {
  en: { action: "Native backend test", find: "Find Codex", discover: "Fetch models", next: "Continue", send: "Send test", close: "Close", login: "Sign in with native Codex", unavailable: "Operation unavailable. The preview made no automatic retry." },
  zh: { action: "原生后台测试", find: "查找 Codex", discover: "获取模型", next: "继续", send: "发送测试", close: "关闭", login: "通过原生 Codex 登录", unavailable: "操作未完成，未自动重试。" },
};
const state = { agents: [{ id: "codex", name: "Codex", icon: "generic", path: "/fixture/codex", fields: [{ key: "model", label: "model", value: "original-model", options: [{ value: "original-model", label: "Original model" }, { value: "relay/fixture", label: "Fixture", ref: "relay/fixture" }] }] }], profiles: [] };
for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: Codex native test stays inside the original row and sheet`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 980, height: 700 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      const calls = [], errors = [];
      let ready = false, discovered = false, failLogin = false, pendingConfigure, enabled = true;
      page.on("pageerror", e => errors.push(e.message));
      const capabilities = () => ({ executableSelected: ready, aiHubConfigured: false, busy: false, canDiscover: ready, canLogin: ready, canSend: discovered });
      await page.route("**/*", async route => {
        const req = route.request(), url = new URL(req.url()), json = data => route.fulfill({ json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:${JSON.stringify(lang)},theme:"light",web:true,nativeCodexPreview:${enabled},nativeCodexToken:"public-fixture-token"};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
        if (url.pathname.startsWith("/api/codex-native/")) {
          assert.equal(req.method(), "POST"); assert.equal(req.headers()["x-preview-token"], "public-fixture-token");
          const action = url.pathname.split("/").pop(), body = req.postDataJSON(); calls.push({ action, body });
          if (action === "auto") { ready = true; return json(capabilities()); }
          if (action === "discover") { assert.equal(body.confirm, true); discovered = true; return json({ models: [{ id: "fixture-model", name: "Fixture model" }] }); }
          if (action === "send") { assert.deepEqual(body, { provider: "openai", model: "fixture-model", text: "public test", confirm: true }); return json({ text: "Synthetic answer <not HTML>" }); }
          if (action === "login" && failLogin) return route.fulfill({ status: 502, json: { error: "unavailable" } });
          if (action === "configure") { pendingConfigure = () => json(capabilities()); return; }
          return json(capabilities());
        }
        if (url.pathname === "/api/state") return json({ ...state, settings: { lang, theme: "light", guideHidden: true } });
        if (url.pathname === "/api/usage/quotas") return json([]);
        if (url.pathname === "/api/groups") return json({ groups: [] });
        if (url.pathname === "/api/plugins") return json({ plugins: [] });
        if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
        if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
        if (url.pathname.startsWith("/api/")) {
          assert.equal(req.method(), "GET", "native actions must not post to old Connect/provider/Sync APIs");
          return json({});
        }
        const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
        return route.fulfill({ body: await fs.readFile(file), contentType });
      });
      const w = words[lang];
      await page.goto("http://magpie.test/?view=agents");
      const row = page.locator('.row.agent[data-id="codex"]');
      await row.getByRole("button", { name: w.action, exact: true }).click();
      const modal = page.locator("#modal .editor");
      await page.waitForFunction(() => !document.querySelector("#nativeResult")?.textContent.includes("…"));
      assert.deepEqual(calls.map(x => x.action), ["state"], "opening starts no native action");
      assert.equal(await row.locator('.ag-conn').getAttribute("aria-checked"), "false", "original Connect state remains unchanged");
      await modal.getByRole("button", { name: w.find, exact: true }).click();
      await modal.getByRole("button", { name: w.discover, exact: true }).waitFor();
      await page.waitForFunction(() => [...document.querySelectorAll('#modal button')].some(b => b.textContent === 'AIHub' && !b.disabled));
      await modal.getByRole("button", { name: w.discover, exact: true }).click();
      assert.equal(calls.filter(x => x.action === "discover").length, 0, "confirmation before discovery");
      await page.locator("dialog.action-confirm").getByRole("button", { name: w.next, exact: true }).click();
      await modal.getByRole("button", { name: "Fixture model", exact: true }).click();
      await modal.locator("textarea").fill("public test");
      await modal.getByRole("button", { name: w.send, exact: true }).click();
      assert.equal(calls.filter(x => x.action === "send").length, 0);
      await page.locator("dialog.action-confirm").getByRole("button", { name: w.next, exact: true }).click();
      await page.getByText("Synthetic answer <not HTML>", { exact: true }).waitFor();
      assert.equal(await modal.locator("not").count(), 0);
      failLogin = true;
      await modal.getByRole("button", { name: w.login, exact: true }).click();
      await page.locator("dialog.action-confirm").getByRole("button", { name: w.next, exact: true }).click();
      await page.getByText(w.unavailable, { exact: true }).waitFor();
      assert.equal(await modal.getByRole("button", { name: "Fixture model", exact: true }).count(), 0, "failed fresh login never reuses old-context models");
      await modal.getByRole("button", { name: "AIHub", exact: true }).click();
      await modal.locator("#nativeEndpoint").fill("https://example.invalid");
      await modal.locator("#nativeKey").fill("PUBLIC-NOT-A-CREDENTIAL");
      await modal.locator("#nativeEndpoint").locator("..").getByRole("button").click();
      await page.locator("dialog.action-confirm").getByRole("button", { name: w.next, exact: true }).click();
      await page.waitForFunction(() => document.querySelector("#nativeEndpoint").disabled);
      assert.equal(await modal.locator("#nativeKey").inputValue(), "");
      assert.equal(await modal.locator("#nativeKey").isDisabled(), true);
      assert.ok(pendingConfigure); await pendingConfigure();
      await page.waitForFunction(() => !document.querySelector("#nativeEndpoint").disabled);
      await modal.getByRole("button", { name: w.close, exact: true }).click();
      assert.equal(calls.filter(x => x.action === "send").length, 1);
      assert.deepEqual(errors, []);
      const missing = await page.evaluate(() => ["zh", "zh-TW", "ja", "de"].filter(l => !I18N[l]["Native backend test"]));
      assert.deepEqual(missing, []);
      enabled = false;
      const before = calls.length;
      await page.reload();
      await row.locator('.ag-conn').waitFor();
      assert.equal(await row.getByRole("button", { name: w.action, exact: true }).count(), 0, "default startup has no native action");
      assert.equal(calls.length, before, "default startup makes no native call");
    });
  }
}
