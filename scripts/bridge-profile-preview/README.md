# Isolated bridge-profile draft UI

This is a small Windows-only **draft preview**, not the ordinary Magpie GUI and
not a finished Codex connection wizard. It runs independently of Magpie's normal
startup, migrations, provider/account discovery, agent wiring and gateway.

Open `preview.ps1` using Windows PowerShell in STA mode, for example:

```powershell
powershell.exe -NoProfile -STA -File .\scripts\bridge-profile-preview\preview.ps1
```

Do not bypass execution policy or other enterprise restrictions to run it.
It is not yet included in an EXE or installer; this command is a developer
verification entry, **not the requested final one-click experience**.

The native file and folder dialogs return real local Windows paths. Choose two
native executable paths and four existing, separate test state directories.
The provider field is the identifier already used by that backend's own
configuration, not a service URL or secret. This utility neither prepares those
directories nor reads their contents. Do not choose an ordinary account's home.

`Preview draft` generates the actual minimal bridge `backends` JSON shape in a
read-only text box. It checks identifiers, local absolute Windows path forms,
`.exe` suffixes and lexical state-directory overlap. Changing any field clears
the previous preview. It does **not** call `Config.Validate`, read binaries or
configuration files, follow symlinks, verify signatures or claim runtime
validation. Junctions, executable identity, file permissions, policy and
compatibility remain unverified. The real bridge's strict `--check` remains
necessary before any separately authorized use, and itself does not prove
Desktop functionality.

There are deliberately no save, connect, launch, login, account import,
environment inheritance, security-mode or advanced compatibility controls.
No clipboard operation is performed. All selected values stay in memory until
the window closes. Windows dialogs may keep ordinary OS-managed navigation
history; this is not a guarantee of zero operating-system writes.

No real-account GPT/AIHub selection or production-ready profile is offered.
The default-off compatibility modes, executable hash pinning, backend policy
setup and normal Codex startup integration are not configured here. The draft
alone will not make the Desktop work. This limitation avoids inventing policy,
silently sharing homes or presenting current no-account experiments as supported
authentication.

## Verification

```powershell
powershell.exe -NoProfile -File .\scripts\bridge-profile-preview\preview.test.ps1
```

Tests dot-source only the pure draft builder. Synthetic nonexistent paths ensure
that generation does not depend on backend files. Tests cover two backend JSON
shape, extra-field stripping, invalid identifiers and paths, same/nested homes,
and a structural capability guard. That guard is not a security boundary.
GUI dialogs, layout, STA behavior and actual Windows execution require a Windows
runner/manual acceptance; no claim of those passing is implied by static review.

The independent `Experimental bridge profile draft tests` workflow runs only
for this experimental branch when the preview scripts or that workflow change.
It checks out the exact triggering commit with read-only repository permissions
and no persisted Git credentials, then runs these pure tests in Windows
PowerShell 5.1. It does not open a window, launch a backend, run the Desktop job,
publish artifacts, or deploy anything. The explicit shell does not override
execution policy; a policy refusal is a failure, not permission to bypass it.
This tests the builder only, not the visual UI or native selectors. No successful
CI run is claimed by adding the workflow.

The next product step is a single isolated compiled entry with these selectors
and explicit bridge-owned persistence, **after** authentication and Desktop
configuration semantics are settled. Do not wire this into the ordinary
`Connect Codex` action, which has different HTTP-gateway configuration semantics.
