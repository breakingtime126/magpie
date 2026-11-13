Read-only Codex executable PATH diagnostic, not a Magpie replacement.
Run once without elevation:
runtime-path-probe.exe "<absolute installed app resources path to codex.exe>"

Only performs Go filepath.EvalSymlinks, cleaned case-insensitive path comparison,
and os.Lstat on the specified path. Does NOT read executable contents or hashes,
launch Codex, read credentials/configuration, contact any service, or write files.
JSON on stdout omits the supplied/resolved path and includes a fixed failure
stage and OS error code. Exit 0 = these path checks passed; exit 1 = check failed;
exit 2 = invalid usage/output error.
A passing probe does not prove Magpie has the same process access, and does not
test later digest, cancellation, version, bridge, provider or account stages.
Built with Go 1.26.8 matching the diagnostic Magpie build toolchain.
