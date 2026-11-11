// No browser needed: execute the actual original switch and its native helpers.
// The browser regression additionally covers the complete page and HTTP fixture.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");
const source = fs.readFileSync(process.env.MAGPIE_SWITCH_SOURCE || path.join(__dirname, "../assets/app.js"), "utf8");
const start = source.indexOf("// This is launch selection, never legacy wiring");
const switchStart = source.indexOf("function connectSwitch(a) {");
const switchEnd = source.indexOf('// ---------- 「接入」', switchStart);
const code = source.slice(start < 0 ? switchStart : start, switchEnd);
function setup(selection) {
  const context = {
    nativeSelection: selection, nativeSelectionBusy: false,
    nativeSelectionAgent: (a) => a.id === "codex", nativeNeedsRestore: () => selection.unknown || selection.journalPresent || selection.recoveryRequired || selection.selectionConfigured,
    t: (s) => s, holdAgentModels() {},
    el: (_tag, cls) => ({ className: cls, attributes: {}, dataset: {}, setAttribute(k, v) { this.attributes[k] = v; }, append() {} }),
  };
  vm.createContext(context);
  vm.runInContext(code, context);
  return context;
}
test("original Codex switch uses configured selection, not false legacy wired", () => {
  const context = setup({ selectionConfigured: true, journalPresent: true });
  const button = context.connectSwitch({ id: "codex", name: "Codex", wired: false });
  assert.equal(button.attributes["aria-checked"], "true");
  assert.equal(button.attributes["aria-label"], "Disconnect and restore Codex launch selection");
  assert.match(button.title, /chat have not been verified/);
});
test("unknown and interrupted selection keep explicit restoration reachable", () => {
  for (const selection of [{ unknown: true }, { journalPresent: true, recoveryRequired: true }]) {
    const context = setup(selection);
    const button = context.connectSwitch({ id: "codex", name: "Codex", wired: false });
    assert.equal(button.attributes["aria-label"], "Disconnect and restore Codex launch selection");
    let restored = false;
    context.askDisconnect = () => { restored = true; };
    button.onclick({ stopPropagation() {} });
    assert.equal(restored, true);
  }
});
test("other agents retain the original wired switch", () => {
  const context = setup({ selectionConfigured: true, journalPresent: true });
  const button = context.connectSwitch({ id: "claude", name: "Claude Code", wired: false });
  assert.equal(button.attributes["aria-checked"], "false");
  assert.equal(button.attributes["aria-label"], "Connect {agent} to magpie");
});

function apiSetup(selection) {
  const context = setup(selection);
  Object.assign(context, {
    nativeSelectionRevision: 0, nativeCodexConnection: true, web: false,
    readsSame: () => false, skelHold: async () => {}, noteAccounts() {},
    renderAgents() {}, saidOk() {}, status() {}, confirmAsk: null, closeConfirmAsk() {},
  });
  vm.runInContext(source.slice(source.indexOf("async function api("), source.indexOf("function notJSON(")), context);
  return context;
}
const response = (selection) => ({ ok: true, status: 200, headers: { get: () => null }, text: async () => JSON.stringify({ agents: [], nativeCodexSelection: selection }) });

test("held old reads cannot undo a completed Connect or Disconnect", async () => {
  for (const [action, before, after] of [
    ["connect", {}, { selectionConfigured: true, journalPresent: true }],
    ["disconnect", { selectionConfigured: true, journalPresent: true }, {}],
  ]) {
    const context = apiSetup(before);
    let release;
    context.fetch = async (url) => url === "/api/state" ? new Promise((resolve) => { release = resolve; }) : response(after);
    const held = context.api("state");
    await context.changeNativeSelection(action);
    release(response(before));
    await held;
    assert.equal(!!context.nativeSelection.selectionConfigured, !!after.selectionConfigured);
    assert.equal(!!context.nativeSelection.journalPresent, !!after.journalPresent);
  }
});

test("recovery refresh keeps mutation busy and cannot close a newer dialog", async () => {
  const context = apiSetup({});
  let release, posted = 0;
  context.fetch = async (url) => {
    if (url === "/api/state") return new Promise((resolve) => { release = resolve; });
    posted++;
    throw new Error("synthetic response lost");
  };
  const first = context.changeNativeSelection("connect");
  while (!release) await new Promise((resolve) => setImmediate(resolve));
  await context.changeNativeSelection("connect");
  assert.equal(posted, 1);
  assert.equal(context.nativeSelectionBusy, true);
  release(response({ journalPresent: true, recoveryRequired: true }));
  await first;
  assert.equal(context.nativeSelection.journalPresent, true);
  let closeCount = 0;
  const oldDialog = {};
  context.confirmAsk = {}; // a newer dialog after Cancel
  context.closeConfirmAsk = () => closeCount++;
  context.fetch = async () => response({});
  await context.changeNativeSelection("disconnect", oldDialog);
  assert.equal(closeCount, 0);
});


test("model-ready native Codex is excluded from legacy Get started chips", () => {
  const context = setup({ selectionConfigured: true, journalPresent: true });
  context.state = { agents: [{ id: "codex", fields: [{ key: "model", options: [{ ref: "aihub/model" }] }] }, { id: "claude" }, { id: "codex@wsl:test" }] };
  context.connectKind = () => "ok"; // all have ready model choices
  const helper = source.slice(source.indexOf("function guideConnectCandidates()"), source.indexOf("function guideCard("));
  vm.runInContext(helper, context);
  assert.deepEqual(Array.from(context.guideConnectCandidates(), (a) => a.id), ["claude", "codex@wsl:test"]);
});
