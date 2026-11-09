# Isolated bridge-profile helper

This independent Windows GUI can generate, save and check a minimal bridge
profile. It never loads ordinary Magpie startup, automatic account discovery, migrations,
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
   launch, authentication attempt, startup probe, clipboard or Desktop change
   from Save/check. Identity inspection below is a separate native operation.
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
global settings or the backend homes through Save/check. A separately consented
identity inspection may change native state; deleting the GUI snapshot does not
undo those effects.

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

### Owned-window CI acceptance

The existing profile workflow additionally builds the real standalone adapter and
shows this helper's own WinForms window in an interactive Windows runner session.
It invokes the owned Save button twice, checks real `--check` success and no
replacement of the first profile, edits a field to invalidate the result, closes
the window, then deletes only this attempt's two new profiles. Four empty backend
homes and the process environment must remain unchanged. The selected fake `.exe`
contains non-executable public text; successful checking does not mean it can run.
No official backend, Desktop, account, service or sandbox setup is launched.

Selectors are substituted with synthetic paths. Standard system dialogs, physical
mouse/keyboard input and the ZIP `.cmd` entry remain unverified. Images are
DrawToBitmap captures of the shown owned control tree, not whole-desktop screen
captures or proof of unobscured visibility. No UIA or coordinate clicking is used.
The checker retains its bounded timeout; CI has an overall 15-minute ceiling and
failure cleanup removes only this test's fresh directories. Runner cancellation
can prevent final cleanup/reporting and must not be reported as rollback success.
A noninteractive session fails rather than claiming GUI acceptance. Local preparation
has no PowerShell runtime, so these new checks require the resulting Windows run.

## Optional primary identity inspection

`Inspect primary identity...` is separate from Save/check and never automatic.
The first provider must be `openai`. Select a separate snapshot parent; the helper
shows the selected primary program, HOME, CODEX_HOME and computed SHA-256, then
asks once with **No** as default. Yes means you confirm trust in the selected
program, its reviewed 0.167.0-alpha.3.2 contract and native configuration, and that
these are dedicated contexts rather than ordinary account homes. There is no
automatic provenance/version/destination verification or account preparation.
The hash checks byte consistency, not official origin or OpenAI-only routing.

This operation starts only the primary through the fixed bundled adapter. It can
read/refresh credentials, contact services and change its own native state even
with includeToken=false and refreshToken=false. No login, logout, chat, proof or
secondary launch is initiated. The helper neither asks for nor displays tokens,
email, plan or native errors. No retry/polling follows failure. A null method means
"native did not report an identity", not "no account exists"; a non-null method
is not proof of valid credentials, quota, chat readiness or Desktop integration.

A newly created, locked snapshot contains only the five displayed fields plus
the primary hash. It lives outside backend homes and is removed after the attempt;
existing bridge.json files are never changed. **This only rolls back GUI-owned
files, not native side effects.** The UI can be unresponsive during the synchronous
operation. It waits up to 30 seconds for the adapter then requests termination and
waits another 3 seconds; stream drains and cleanup are additional work, not a hard
whole-operation deadline. Failure, timeout or cleanup uncertainty never reports an
identity. Inherited native cleanup limits are in the core inspection design;
there is no claim that every descendant or native state change is undone.
The ZIP includes `PRIMARY-IDENTITY.md` with the core protocol and cleanup contract.

The existing owned-window test uses the core's copied Go test executable, public
synthetic responses and separate test homes. It exercises consent-decline with no
launch, one approved synthetic identity exchange, fixed projection, primary-only
routing, snapshot removal, old-profile preservation and field invalidation. The
consent dialog and selectors are substituted and **not** UI-accepted. Original
Save/check homes retain their independent unchanged checks; identity homes are
allowed to acquire synthetic logs. No official runtime or real account is used.
These new identity paths await Windows CI and do not extend the previously passed
save/check/rollback result to real authentication.
