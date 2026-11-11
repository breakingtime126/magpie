// Original Agents control, with a synthetic owning-host API. No live gateway,
// native transport, credentials or registry. Go tests enforce host ownership.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = process.env.MAGPIE_RECOVERY_ASSETS || path.resolve(__dirname, "../assets");
const off = () => ({ journalPresent: false, selectionConfigured: false, recoveryRequired: false, unknown: false, backendHandshakeVerified: false, chatVerified: false });
const configured = () => ({ ...off(), journalPresent: true, selectionConfigured: true, restoreAvailable: true });
const row = '.row.agent[data-id="codex"]';
const sw = row + " .ag-conn";
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"]) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: original Codex selection survives reload and uncertain recovery`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch(process.env.MAGPIE_CHROMIUM_EXECUTABLE ? { executablePath: process.env.MAGPIE_CHROMIUM_EXECUTABLE } : { channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 420, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      let selection = off(), missing = false, partial = false, conflict = false, heldState;
      const posts = [], errors = [], unexpected = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const state = () => ({ agents: missing ? [] : [{ id: "codex", name: "Codex", icon: "generic", wired: false, fields: [{ key: "model", label: "model", value: "gpt-5.4", options: [{ value: "gpt-5.4", label: "GPT-5.4" }, { value: "aihub/model", label: "Supplier model", ref: "aihub/model" }] }] }], profiles: [], served: 1, nativeCodexSelection: selection, settings: { lang, theme: "light", guideHidden: false, agentsHidden: ["codex"] } });
      await page.route("**/*", async (route) => {
        const req = route.request(), url = new URL(req.url());
        const json = (data, status = 200) => route.fulfill({ status, json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs=${JSON.stringify({ lang, theme: "light", nativeCodexConnection: true })};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
        if (url.pathname === "/api/state") {
          if (url.searchParams.has("held")) {
            const snapshot = state();
            return new Promise((resolve) => { heldState = async () => { await json(snapshot); resolve(); }; });
          }
          return json(state());
        }
        if (url.pathname === "/api/agents/connect/codex") {
          posts.push("connect");
          selection = partial ? { ...off(), journalPresent: true, recoveryRequired: true, restoreAvailable: true } : configured();
          if (partial) return route.abort("failed"); // write happened, response lost
          return json(state());
        }
        if (url.pathname === "/api/agents/disconnect/codex") {
          posts.push("disconnect");
          if (conflict) return json({ error: "synthetic external-change conflict", nativeCodexSelection: selection }, 400);
          selection = off();
          return json(state());
        }
        if (/preview|codex-native/.test(url.pathname)) unexpected.push(url.pathname);
        if (url.pathname === "/api/usage/quotas") return json([]);
        if (url.pathname === "/api/groups") return json({ groups: [] });
        if (url.pathname === "/api/plugins") return json({ plugins: [] });
        if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
        if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
        if (url.pathname.startsWith("/api/")) return json({});
        const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        return route.fulfill({ body: await fs.readFile(file), contentType: { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml" }[path.extname(file)] });
      });
      await page.goto("http://magpie.test/?view=agents");
      await page.waitForFunction((s) => document.querySelector(s)?.dataset.selectionState === "off" && !document.querySelector(s).disabled, sw);
      await page.evaluate(() => { window.heldSelectionRead = api("state?held=1"); });
      for (let i = 0; !heldState && i < 1000; i++) await new Promise((resolve) => setTimeout(resolve, 5));
      assert.ok(heldState, "held state request reached fixture");
      await page.locator(sw).click();
      await page.waitForFunction((s) => document.querySelector(s)?.getAttribute("aria-checked") === "true", sw);
      await heldState();
      heldState = null;
      await page.evaluate(async () => { state = await window.heldSelectionRead; renderAgents(); });
      assert.equal(await page.locator(sw).getAttribute("aria-checked"), "true");
      assert.deepEqual(posts, ["connect"]);
      assert.equal(await page.locator('.guide-agent[data-id="codex"]').count(), 0);
      assert.equal(selection.chatVerified, false);
      if (lang === "en") assert.match(await page.locator(row).innerText(), /Backend startup and chat have not been verified/);
      // A new window receives journal evidence, not wired or a local remembered toggle.
      await page.reload();
      await page.waitForFunction((s) => document.querySelector(s)?.getAttribute("aria-checked") === "true", sw);
      await page.locator(sw).click();
      assert.match(await page.locator(".disconnect-ask").innerText(), /CODEX_CLI_PATH/);
      await page.locator(".disconnect-ask .bar button").first().click(); // Cancel
      assert.deepEqual(posts, ["connect"]);
      await page.evaluate(() => { window.heldSelectionRead = api("state?held=1"); });
      for (let i = 0; !heldState && i < 1000; i++) await new Promise((resolve) => setTimeout(resolve, 5));
      assert.ok(heldState, "held state request reached fixture");
      await page.locator(sw).click();
      await page.locator(".disconnect-ask .danger-fill").click();
      await page.waitForFunction((s) => document.querySelector(s)?.getAttribute("aria-checked") === "false", sw);
      await heldState();
      heldState = null;
      await page.evaluate(async () => { state = await window.heldSelectionRead; renderAgents(); });
      assert.equal(await page.locator(sw).getAttribute("aria-checked"), "false");
      assert.deepEqual(posts, ["connect", "disconnect"]);
      partial = true;
      await page.locator(sw).click();
      await page.waitForFunction((s) => document.querySelector(s)?.dataset.selectionState === "recovery" && !document.querySelector(s).disabled, sw);
      // Provider/runtime gone, status unknown/conflicting: restoration remains reachable.
      missing = true;
      selection = { ...selection, unknown: true, restoreAvailable: false };
      conflict = true;
      await page.reload();
      await page.locator(sw).click();
      await page.locator(".disconnect-ask .danger-fill").click();
      await page.waitForFunction(() => document.querySelector(".disconnect-ask .danger-fill")?.disabled === false);
      assert.equal(await page.locator(".disconnect-ask").count(), 1);
      assert.equal(selection.journalPresent, true);
      conflict = false;
      await page.locator(".disconnect-ask .danger-fill").click();
      await page.waitForFunction(() => !document.querySelector(".disconnect-ask"));
      assert.deepEqual(posts, ["connect", "disconnect", "connect", "disconnect", "disconnect"]);
      assert.deepEqual(unexpected, []);
      assert.deepEqual(errors, []);
    });
  }
}
