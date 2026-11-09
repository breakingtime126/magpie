# Isolated bridge-profile helper

This independent Windows GUI can generate, save and check a minimal bridge
profile. It never loads ordinary Magpie startup, account discovery, migrations,
agent sync, Connect/Disconnect or the HTTP gateway. It is **not yet a complete
Codex connection or ready-to-chat wizard**.

## Windows ZIP entry

Open `Open-Bridge-Profile.cmd` in the experimental Windows ZIP. It runs the
bundled `bridge-profile.ps1` using standard Windows PowerShell 5.1, NoProfile
and STA. It does not bypass execution policy, unblock downloaded files, elevate,
install anything, or change user/global environment. If enterprise/PowerShell
policy blocks it, stop; artifact availability is not deployment authorization.
The script remains usable for developer preview via standard
`powershell.exe -NoProfile -STA -File preview.ps1`; saving outside the packaged
ZIP will remain unchecked if its fixed companion adapter/BUILD.json are absent.

1. Choose two official native executable paths, provider identifiers and four
   **existing separately prepared test directories**. No account files are read
   or imported. Do not select ordinary account homes or enter keys/tokens.
2. `Preview draft` retains the original pure in-memory JSON builder.
3. `Save and check` asks for an existing, separate profile parent directory.
   It creates a fresh `magpie-bridge-profile-<random>` directory and fixed
   `bridge.json` using CreateNew; existing profiles are never overwritten.
4. Only the fixed companion `magpie-codex-bridge-windows-amd64.exe --check` runs.
   Its bytes must match `BUILD.json`'s nativeAdapterSHA256 first. This verifies
   package consistency, **not signature, publisher or trusted origin**.
   The profile path is supplied only to that child's minimal environment.
5. The UI distinguishes passed, failed and timeout. A failed check leaves a
   saved **unchecked draft**, not a connected profile. There is no retry, backend
   launch, authentication attempt, startup probe, clipboard or Desktop change.
   Editing any field clears the displayed check result; old saved files remain.

The helper rejects nonlocal paths, drive-root selections, junction/symlink
ancestors, and profile parents overlapping any selected backend state directory.
Only the five displayed fields may be saved; secret/env/compatibility fields
are not accepted. Paths are checked before creation and the profile file remains
open without write/delete sharing during validation. These are ordinary owned-
workspace checks, not an OS boundary against hostile concurrent same-user path
replacement. Separate homes do not isolate credentials from same-user processes.

To remove this helper's outputs, close it and delete only its newly created
profile directory and the extracted ZIP directory. It has not changed Codex,
global settings or the backend homes.

## What the check does and does not establish

The real adapter loads this JSON and calls `Config.Validate`: field shape,
existing native-file and directory paths, executable/self checks, and resolved
home overlap. It does not start either backend or read native account/config
contents. A successful check is **not** authentication, provider availability,
model service, executable provenance or Desktop integration acceptance.

The smallest actual chat setup still needs legitimate provider definitions and
credentials in those separately prepared native contexts, reviewed compatibility
settings and a supported Desktop startup path. This helper supplies none of
those and never silently enables the new trusted-token relay or other modes.
It reduces JSON editing, but does not pretend those remaining steps are solved.

## Verification

`preview.test.ps1` reuses the original pure draft tests, then writes only fresh
temporary synthetic directories and replaces the checker with an in-process
fixture. It covers saved JSON, no-overwrite, write locking during check,
failed/timeout outcomes, home overlap, unsupported extra fields and unchanged
process-global environment. It never executes the fake .exe, real adapter,
backend or account login. Static capability checks are regressions, not sandbox
proofs. The existing preview workflow and Windows package job run these tests;
no new workflow or test framework is added.

The actual adapter already has separate unit/lifecycle `--check` coverage. The
new combined GUI, selectors, package-launch behavior and actual GUI-to-adapter
flow still need Windows acceptance. Neither static review nor mocked success
proves those passed. Local preparation had no usable PowerShell runtime, so its
new tests were not run locally.
