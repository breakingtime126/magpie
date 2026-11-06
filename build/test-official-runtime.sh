#!/usr/bin/env bash
# Build/download preparation may use the network. All Codex execution happens
# inside a fresh, unprivileged, network-none container with synthetic state only.
set -euo pipefail

source_commit=4f093f2748667f3b0e6a07e3d4055121cbd3ce64
archive_sha256=c9400b373a2c17c71f48a6af592e8c92b9b212295182a482bf0b351e137c8967
catalog_blob=489ebe3f2483348641a16160ce8491dd077eabca
image=docker.io/library/python@sha256:f3dac913ab53b78c64f26d5518fcd7043d5a1a46522d6a493b598c3ed12f62a6
scratch="$(mktemp -d "${RUNNER_TEMP:?}/official-runtime.XXXXXX")"
container=""
cleanup() {
  if [[ -n "$container" ]]; then docker rm --force "$container" >/dev/null; fi
  chmod -R u+w "$scratch"
  rm -rf "$scratch"
}
trap cleanup EXIT
mkdir "$scratch/inputs"
curl --fail --location --proto '=https' --proto-redir '=https' --max-time 180 \
  --max-filesize 536870912 \
  "https://github.com/openai/codex/releases/download/rust-v0.166.0-alpha.6/codex-x86_64-unknown-linux-musl.tar.gz" \
  --output "$scratch/codex.tar.gz"
printf '%s  %s\n' "$archive_sha256" "$scratch/codex.tar.gz" | sha256sum --check --strict
python3 - "$scratch/codex.tar.gz" "$scratch/inputs/codex" <<'PY'
import pathlib
import sys
import tarfile

with tarfile.open(sys.argv[1], "r:gz") as archive:
    files = [member for member in archive.getmembers() if member.isfile()]
    assert len(files) == 1, "unexpected official release archive layout"
    member = files[0]
    assert pathlib.PurePosixPath(member.name).name == "codex-x86_64-unknown-linux-musl"
    assert 0 < member.size <= 536870912
    content = archive.extractfile(member)
    assert content is not None
    output = pathlib.Path(sys.argv[2])
    output.write_bytes(content.read())
    output.chmod(0o755)
PY
curl --fail --location --proto '=https' --proto-redir '=https' --max-time 60 \
  --max-filesize 10485760 \
  "https://raw.githubusercontent.com/openai/codex/$source_commit/codex-rs/models-manager/models.json" \
  --output "$scratch/inputs/models.json"
test "$(git hash-object "$scratch/inputs/models.json")" = "$catalog_blob"
CGO_ENABLED=0 go build -trimpath -tags nogui -o "$scratch/inputs/magpie-codex-bridge" ./cmd/magpie-codex-bridge
cp build/test-official-runtime.py "$scratch/inputs/test-official-runtime.py"
chmod 755 "$scratch" "$scratch/inputs"
chmod 644 "$scratch/inputs/models.json" "$scratch/inputs/test-official-runtime.py"
sha256sum "$scratch/inputs/codex" "$scratch/inputs/magpie-codex-bridge"
docker pull --platform linux/amd64 "$image"
container="$(docker create --platform linux/amd64 --network none \
  --user 65534:65534 --cap-drop ALL --security-opt no-new-privileges \
  --read-only --pids-limit 128 --memory 2g --cpus 2 \
  --tmpfs /tmp:rw,nosuid,nodev,size=536870912,mode=1777 \
  --mount "type=bind,src=$scratch/inputs,dst=/inputs,readonly" \
  --workdir /tmp --env PYTHONDONTWRITEBYTECODE=1 \
  "$image" python3 -u /inputs/test-official-runtime.py)"
docker inspect "$container" | python3 -c '
import json, sys
c = json.load(sys.stdin)[0]
h = c["HostConfig"]
assert h["NetworkMode"] == "none"
assert h["ReadonlyRootfs"] and not h["Privileged"]
assert h["CapDrop"] == ["ALL"] and not h.get("CapAdd")
assert not h.get("PortBindings")
assert h["PidsLimit"] == 128 and h["Memory"] == 2147483648
assert h["NanoCpus"] == 2000000000
assert set(h["Tmpfs"]) == {"/tmp"}
assert all(flag in h["Tmpfs"]["/tmp"].split(",") for flag in ("nosuid", "nodev"))
allowed_env = {"PATH", "LANG", "GPG_KEY", "PYTHON_VERSION", "PYTHON_SHA256", "PYTHONDONTWRITEBYTECODE"}
assert all(item.split("=", 1)[0] in allowed_env for item in c["Config"]["Env"])
assert "no-new-privileges" in h["SecurityOpt"]
assert c["Config"]["User"] == "65534:65534"
assert not h.get("Devices") and not h.get("DeviceRequests")
assert h["PidMode"] != "host" and h["IpcMode"] != "host"
assert set(c["NetworkSettings"]["Networks"]) == {"none"}
mounts = c["Mounts"]
binds = [m for m in mounts if m["Type"] == "bind"]
assert len(binds) == 1
assert binds[0]["Destination"] == "/inputs" and not binds[0]["RW"]
assert binds[0]["Source"] == sys.argv[1]
assert all(m["Destination"] in {"/inputs", "/tmp"} for m in mounts)
print("Verified: network-none container; no host credentials/socket mounts")
' "$scratch/inputs"
timeout --signal=TERM --kill-after=10s 300s docker start --attach "$container"
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0
test -z "$(git status --porcelain)"
