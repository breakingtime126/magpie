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
there is no retry, automatic logout or state deletion. Restarting the preview does
not resume the previous temporary login context.

**Short-text sending is currently unavailable.** Its button stays disabled until
a proven boundary disabling all local tools is complete. The UI must not silently
substitute API chat, prompts, read-only mode or synthetic results. It is therefore
not yet the requested full account-plus-short-reply acceptance. No ordinary
account is imported. Model discovery may use cache and is not account proof.

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
sandbox setting is exercised by the workflow. Source preparation has no usable
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
