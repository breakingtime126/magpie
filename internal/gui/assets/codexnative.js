// The original Agents row opens its original editor, not another app or page.
// Only the explicitly enabled native namespace may receive these operations.
(() => {
  const enabled = () => window.bootPrefs?.nativeCodexPreview === true;
  const token = () => window.bootPrefs?.nativeCodexToken;
  async function request(action, body = {}) {
    if (!enabled() || typeof token() !== "string" || !token()) throw new Error("unavailable");
    const response = await fetch("/api/codex-native/" + action, {
      method: "POST", headers: { "Content-Type": "application/json", "X-Preview-Token": token() },
      body: JSON.stringify(body), cache: "no-store",
    });
    if (!response.ok) throw new Error("unavailable");
    return response.json();
  }
  window.codexNativeAction = (a, who) => {
    if (!enabled() || a.id !== "codex" || mode === "panel") return;
    const button = el("button", "text action", t("Native backend test"));
    button.type = "button";
    button.onclick = (event) => { event.stopPropagation(); openNative(); };
    who.append(button);
  };
  function openNative() {
    let busy = false, closed = false, supplier = "openai", current = {}, chosen = "", configured = false;
    const models = { openai: [], aihub: [] };
    const editor = el("div", "editor"), head = el("div", "ehead"), body = el("div", "ebody");
    const dismiss = el("button", "text", t("Close"));
    head.append(el("b", "", t("Native backend test")), dismiss);
    const note = el("p", "note", t("Tests use separate native contexts. This does not connect Codex Desktop or change its model menu."));
    const program = el("div", "bar"), find = el("button", "text", t("Find Codex")), choose = el("button", "text", t("Choose program…"));
    program.append(find, choose);
    const providers = el("div", "chips"), official = el("button", "chip", "OpenAI / Codex"), hub = el("button", "chip", "AIHub");
    providers.append(official, hub);
    const address = input("", t("AIHub HTTPS address")), key = input("", t("AIHub key (not saved)"), "password");
    address.id = "nativeEndpoint"; key.id = "nativeKey"; key.autocomplete = "off";
    address.setAttribute("aria-label", t("AIHub HTTPS address")); key.setAttribute("aria-label", t("AIHub key (not saved)"));
    const credentials = el("div", "fields"), configure = el("button", "text", t("Use this service"));
    credentials.append(address, key, configure);
    const actions = el("div", "bar"), login = el("button", "text", t("Sign in with native Codex")), discover = el("button", "text", t("Fetch models"));
    actions.append(login, discover);
    const list = el("div", "mchips"), text = el("textarea", "input");
    text.maxLength = 512; text.rows = 3; text.setAttribute("aria-label", t("Short test message")); text.placeholder = t("Short test message");
    const result = el("p", "note"); result.setAttribute("role", "status"); result.id = "nativeResult";
    const bar = el("div", "bar"), stop = el("button", "text", t("Stop")), send = el("button", "text primary", t("Send test"));
    bar.append(stop, send);
    body.append(note, program, providers, credentials, actions, list, text, result);
    editor.append(head, body, bar);
    function render() {
      if (closed) return;
      official.classList.toggle("on", supplier === "openai"); hub.classList.toggle("on", supplier === "aihub");
      official.setAttribute("aria-pressed", String(supplier === "openai")); hub.setAttribute("aria-pressed", String(supplier === "aihub"));
      credentials.hidden = supplier !== "aihub"; login.hidden = supplier !== "openai";
      for (const field of [address, key, text, official, hub, find, choose, configure]) field.disabled = busy;
      login.disabled = busy || !current.canLogin;
      discover.disabled = busy || !current.canDiscover || (supplier === "aihub" && !configured);
      send.disabled = busy || !current.canSend || !chosen || !text.value.trim() || (supplier === "aihub" && !configured);
      stop.disabled = !busy;
      list.replaceChildren();
      for (const model of models[supplier]) {
        const button = el("button", "mchip" + (chosen === model.id ? " on" : ""), model.name || model.id);
        button.title = t("Discovered, not verified"); button.disabled = busy;
        button.setAttribute("aria-pressed", String(chosen === model.id));
        button.onclick = () => { chosen = model.id; render(); };
        list.append(button);
      }
    }
    function clearSupplier() { models[supplier] = []; chosen = ""; }
    async function run(action, payload = {}, after = () => {}) {
      if (busy || closed) return;
      busy = true; result.textContent = t("Working…"); render();
      try {
        const response = await request(action, payload);
        if (closed) return;
        after(response);
        if (action !== "state") current = await request("state");
        if (!closed) result.textContent ||= t("Operation finished; only this result was checked.");
      } catch { if (!closed) result.textContent = t("Operation unavailable. The preview made no automatic retry."); }
      finally { busy = false; if (!closed) render(); }
    }
    async function ask(title, message, operation) {
      if (busy || closed) return;
      if (await confirmAction(t(title), t(message), t("Continue"))) {
        if (!closed && !busy) operation();
      }
    }
    find.onclick = () => run("auto", {}, () => { models.openai = []; models.aihub = []; chosen = ""; result.textContent = t("Program selected, not a verified connection."); });
    choose.onclick = () => run("choose", {}, () => { models.openai = []; models.aihub = []; chosen = ""; result.textContent = t("Program selected, not a verified connection."); });
    official.onclick = () => { supplier = "openai"; chosen = ""; render(); };
    hub.onclick = () => { supplier = "aihub"; chosen = ""; render(); };
    address.oninput = key.oninput = () => { configured = false; models.aihub = []; chosen = ""; render(); };
    text.oninput = render;
    configure.onclick = () => ask("Use this service", "The key will be held in memory for this preview and sent only when you explicitly ask this service. Trust this HTTPS address before continuing.", () => {
      const payload = { endpoint: address.value, key: key.value, confirm: true };
      key.value = ""; configured = false; clearSupplier();
      run("configure", payload, () => { configured = true; result.textContent = t("Service selected, not a verified connection."); });
    });
    login.onclick = () => ask("Sign in with native Codex", "Native login uses a new context and may contact services, save or revoke authentication under managed storage rules. Complete it in the browser. Cancel or failure cannot undo those changes; old contexts remain. Separate folders are not OS isolation.", () => {
      models.openai = []; chosen = "";
      run("login", { confirm: true }, () => { result.textContent = t("Native login exited normally. This is not a chat or account validation."); });
    });
    discover.onclick = () => ask("Fetch models", "The selected native program may read or refresh this context's credentials, contact services and write state. Cancel cannot undo those effects. A model list is not proof that a credential or model works.", () => {
      clearSupplier();
      run("discover", { provider: supplier, confirm: true }, (response) => {
        models[supplier] = response.models || []; result.textContent = t("Discovered, not verified");
      });
    });
    send.onclick = () => ask("Send test", "Send this short message to the selected service and model? It may incur service charges. This is a new one-turn native conversation, not a Codex Desktop conversation. Cancel cannot undo a sent request or charge.", () => {
      run("send", { provider: supplier, model: chosen, text: text.value, confirm: true }, (response) => { result.textContent = response.text; });
    });
    stop.onclick = () => {
      stop.disabled = true;
      request("cancel").then(() => {
        if (!closed && busy) result.textContent = t("Cancellation requested; waiting for the operation to finish.");
      }, () => { if (!closed) result.textContent = t("Cancellation outcome unknown; no automatic retry."); });
    };
    const cleanup = () => {
      if (closed) return;
      closed = true; key.value = ""; observer.disconnect();
      if (busy) request("cancel").catch(() => {});
    };
    dismiss.onclick = () => { cleanup(); closeModal(); };
    const observer = new MutationObserver(() => { if (!editor.isConnected || $("#modal").hidden) cleanup(); });
    openModal(editor);
    observer.observe($("#modal"), { attributes: true, childList: true, subtree: true });
    render();
    // Opening the sheet reads controller state only; no native child is started.
    run("state", {}, (response) => { current = response; result.textContent = t("Choose a program and an explicit operation. Existing accounts are not imported."); });
  }
})();
