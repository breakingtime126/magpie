# Magpie Codex Connectivity Preview (experimental)

Open **Magpie-Codex-Connectivity-Preview.exe**. This is a real independent Wails
Windows GUI, not the previous command-line binary or internal-path form. It needs
the system's Microsoft Edge WebView2 Runtime. If missing or blocked by company
policy, stop and use the organization's approved installation process; this
preview does not download runtimes, elevate or bypass policy.

## What works in this slice

Use Auto find to locate the current user's uniquely identified installed package.
A failure or ambiguity is not proof that Codex is absent; manual selection remains
available. Neither route runs the executable during selection. Choose a program
you trust. The program creates its own separate
native contexts; you never enter backend IDs, providers, homes, pins or JSON.
For AIHub, enter your trusted HTTPS service address and key, then explicitly use
that service. The key is held for this process and is not intentionally saved by
the preview. Discovery starts the selected native program only after confirmation
and returns actual model names, labelled **discovered, not verified**. It is not
proof that a model request or credential works; native catalogues can be cached.
A program selection/hash is not official provenance or version verification.

Official login is an explicit handoff: after consent, the selected native program
runs its own browser login, in a new dedicated context on every attempt. You finish
in the browser. Normal native exit is not a separate account, quota or chat check.
Native managed storage rules still apply; login may clean/revoke authentication,
contact services and write state. Separate homes are not an OS boundary and do
not prove that system credential stores are unaffected. Cancelling leaves state;
the preview does not automatically retry, log out or delete state. Restarting the preview does
not resume the previous temporary login context.

**One short native response is an experimental, explicitly requested action.**
Discover and choose a model first. Only the reviewed 0.167.0-alpha.3.2 native
version enables the candidate path; 0.166.0-alpha.6 remains discovery/login only.
Send starts a new ephemeral thread, not a continuing chat. In the same native
process it rechecks the effective provider, selected model and supported feature
gates before requesting the thread and turn with empty environments. A managed
policy conflict or unknown protocol response stops rather than weakening policy.
No Windows sandbox setup, approval-policy change or tool approval is performed.
The response uses the native backend's own model, not API-chat substitution or
synthetic output. A model list can still be cached and is not account proof.

Only a completed, owner-correlated native turn's final text is displayed. A failed
new attempt clears the old reply rather than presenting it as the new result.
Cancellation or a timeout may happen after transmission or billing; neither
retracts the request. The preview/controller does not automatically retry, but
native transport or model loops may retry; this is not a one-HTTP-request or
fixed-cost guarantee. Native startup/discovery
can have authentication/network/state effects before later guards are checked.
This slice is not a promise of zero network, zero writes, all tools absent from
the registry, or OS credential isolation. Hosted-account requests remain untested
in development; synthetic protocol success is not live provider acceptance.

Discovery can refresh credentials, make network calls and modify native state.
Cancel stops the current attempt without retry, but cannot undo those effects.
A fresh login attempt clears the old official-model list even on failure or
cancellation, since the controller has selected a new primary context.

Closing requests cancellation of the preview-owned native operation; a browser
opened by native login remains yours and is not terminated. Closing does not log
out, revoke or automatically delete native state. The ordinary Codex launcher,
global settings and existing history are not edited. The AIHub key is not
intentionally saved and is dropped from controller state at close. New login
contexts can retain authentication and partial state; handle these explicitly
instead of assuming closing the window undoes them. Browser
and native process memory are ordinary memory, not securely zeroed storage.
The GUI uses its own WebView data directory and does not claim forensic zero-
persistence of transient input. Never use a credential with an untrusted endpoint.

## Architecture boundary

`cmd/magpie-codex-setup` calls only `internal/codexsetup` and the independent
`internal/nativepreview` controller. It never invokes ordinary `cli.Main`,
`gui.Run`, `gui.Handler`, Connect/Disconnect, migration, Sync or HTTP gateway.
It does not configure Codex Desktop's launcher or promise same-window integration.
The two-card page is original; its palette/card styling reuses MIT Magpie CSS.
It does not load the original application JS or plugin host.

Wails serves embedded assets and a small handler in process, not a
public TCP server. Every API request requires the per-window random request token.
A supplied Origin must exactly equal the fixed production Windows Wails origin
`http://wails.localhost`; absent Origin still needs the token. Foreign origins, missing confirmation, unknown fields and unavailable capabilities fail.
Source errors, keys, endpoint credentials, account email and plan are never
returned by that handler or intentionally logged. Responses render as text, not
HTML. `nativepreview` remains responsible for native lifetime, destination and
credential scope, model ownership, bounds and cancellation.

## Build and evidence

The scoped Windows workflow builds only this GUI with `production` and
`-H windowsgui`, never `nogui` or the ordinary Magpie release workflow. Wails
entry files use `windows && !nogui`; pure handler/controller code remains
available to the existing platform tests. The package contains only this EXE,
license notices, this document, BUILD.json and SHA256SUMS. It is unsigned.

CI runs synthetic controller/handler tests and launches this own empty window
without choosing a native executable, account, provider or safety mode. Its
PrintWindow image is evidence to inspect, not proof of control usability or a
successful login/chat. A window title or successful build is not interface
acceptance. No official Desktop, real native account, external service or Windows
sandbox setting is exercised by the GUI workflow. Source preparation has no usable
Go/PowerShell toolchain, so new compile/runtime tests remain unrun until CI.

The pinned Wails beta.24 loader reads the existing WebView2 installation and
loads its DLL; this preview does not install or download it. A missing/unknown
runtime stops with a native notice. Wails itself clears process-local WebView2
pipe/browser-argument/browser-folder/user-data overrides and sets its release
channel preference. Corresponding loader registry overrides can be suppressed;
this is not a registry write or a claim to override all enterprise Group Policy.
No custom proxy, certificate or system-network setting change is added here.

The Linux synthetic format/vet/race job and Windows build/window job run in
parallel to expose failures together. Both must succeed before a package is
recommended. A formatted-copy artifact is diagnostic output, not a changed or
silently reformatted source checkout. This independent entry uses production
Wails and `-H windowsgui`, not the ordinary main's `comlauncher` self-installation
flow. It deliberately does not embed that launcher's resources or install logic.

## 中文快速开始

1. 解压后打开 `Magpie-Codex-Connectivity-Preview.exe`，点“自动查找”。
2. 官方账号由你点“官方登录”并在原生浏览器页面完成；AIHub 只填你信任的服务地址和密钥。
3. 点“发现模型”后选一个模型；若当前版本允许，点“确认后发送一次”执行一条短文本测试。每次是新对话，可能计费。

不是 Codex Desktop 同窗口成品，不会替你修改原入口或迁移历史。关闭窗口会请求停止本次操作，但不会替你注销、删除新登录状态、关闭浏览器或撤回已经发送的请求。本预览不自动重试，但原生传输或模型循环可能重试，并非固定费用保证。密钥不默认保存，独立目录不等于操作系统安全隔离。没有 WebView2 或公司策略拒绝时停止，不绕过限制。
