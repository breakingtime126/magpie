# Official Desktop cold-start CI

The experimental `Experimental Desktop cold start` workflow is a separate,
credential-free baseline on a fresh GitHub-hosted Windows 2025 VM. It never
installs or runs anything on a user's computer, changes `main`, publishes a
release, or imports account state.

It downloads the current x64 Store-signed package and offline license from the
[official Windows deployment page](https://learn.chatgpt.com/docs/enterprise/windows-deployment),
verifies trust with the Windows SDK signature tool, records SHA-256 and manifest
identity/version, and checks the existing runner's interactive desktop. The URL
is floating. The first run verified 26.1103.3517.0, SHA-256
`ca7c8896e11145b9c34ec8ed6e701b7f62d229fe05cd01d2542064b49f1cd643`.
Subsequent runs stop if those bytes change. This is not a reproduction of
historical Desktop 26.1029.7803.0. The verified manifest has three applications;
the Desktop entry is selected by its exact `app/ChatGPT.exe` executable, and its
AUMID still comes from actual registration plus manifest application ID.

Only the documented licensed provisioning command is used. No unsigned-package
switch, license bypass, manifest change, certificate installation, security or
network policy change, new session, RDP, account login, or repackaged executable
is allowed. If provisioning does not register the application in the current
runner session, the probe stops and reports that limitation.

Activation uses the registered AUMID and Windows' application activation manager.
Process package identity, visible window geometry and a bounded screenshot are
separate evidence. Each launch is observed for 60 seconds without UI interaction. A screenshot can
be occluded and must be reviewed; a green
job means only that those baseline observations were obtained. It is not a
successful bridge connection, usable model picker, account login, model response,
or production acceptance.

The first phase deliberately does not set `CODEX_CLI_PATH`. Normal MSIX activation
has no per-launch environment-block parameter, so setting an environment variable
in PowerShell does not prove that Desktop receives it. The second job uses another fresh VM and the same SHA-pinned official package.
It launches only the newly registered `chatgpt-desktop.exe` alias declared under
that manifest's Desktop application. An identically named alias existing before
installation is a blocker. Directory-entry enumeration checks the documented
root alias location as well as the family-qualified location; the initial `Test-Path` check failed to discover the latter, while subsequent
directory enumeration found both. The selected entry must have the documented
CreateProcess alias reparse tag `0x8000001B`. The probe never parses undocumented
APPEXECLINK payload strings or fabricates an alias. Actual runtime package identity
remains a separate mandatory check. Non-shell process
creation supplies a minimal, non-secret, process-local environment containing
the exact commit-built bridge path. It never writes user/system environment.

The pinned package contains both `app\\Codex.exe` and
`app\\resources\\codex.exe`. Only the latter resource backend is considered, and
its `--version` must identify it as `codex-cli` in a separate empty home with no
inherited authentication environment. This is an identity/version check, not a
model or authentication request.

The bridge configuration has two new empty homes and loopback discard endpoints,
no API key, and no startup, requirements, attestation or OAuth opt-ins for this
newer unreviewed runtime. The exact production bridge is built unchanged with a
unique commit-derived filename and validated using `--check` before monitoring.
The probe requires a matching child process-start event under a package-verified
Desktop parent, and records file hashes before/after. Short-lived events may not
permit querying the running image path; this evidence is not proof of a successful
protocol or model request. Static adapter error markers may be detected in fresh
app log files; no raw log lines, auth files or arbitrary environment are emitted. Do not substitute naked extracted EXEs, global environment changes or
debug injection to make this test appear to pass.

A screenshot of at most 1 MiB is also emitted as base64 in workflow logs for
remote visual review where supported. Workflow-log retention is independent of
the seven-day artifact retention; no successful image retrieval is assumed.

Artifacts contain only public package/signature metadata, sanitized test-process
facts and the fresh unauthenticated window image. They do not contain the MSIX,
offline license, app data, credentials, raw process command lines, or environment
values. Test-created activation is closed when possible; the disposable VM is
the final process and filesystem lifetime boundary.
