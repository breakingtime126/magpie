#!/usr/bin/env python3
"""build/test-official-runtime.py: synthetic-only, isolated official-runtime CI.

Requires a non-root Linux container with --network=none. All writable state is
fresh under /tmp. Inputs are read-only /inputs/{codex,magpie-codex-bridge,models.json}.
No real credentials, platform proofs, remote services, or debug-only flags.
"""
import base64
import contextlib
import copy
import datetime
import hashlib
import http.server
import json
import os
import pathlib
import queue
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import threading
import time

MAX = 4 * 1024 * 1024
TIMEOUT = 45
MODEL = "gpt-5.5"
MARKER = "SYNTHETIC-NONPROOF-CI-MARKER"
CODEX = "/inputs/codex"
BRIDGE = "/inputs/magpie-codex-bridge"
VERSION = "0.166.0-alpha.6"
PEERS = []
REPORT = {"scope": "synthetic-loopback-only", "checks": []}


def need(condition, message):
    if not condition:
        raise AssertionError(message)


def encode(value):
    return json.dumps(value, separators=(",", ":")).encode()


def b64(data):
    return base64.urlsafe_b64encode(data).decode().rstrip("=")


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def clean_env(home, codex_home):
    return {
        "HOME": str(home), "USERPROFILE": str(home),
        "CODEX_HOME": str(codex_home),
        "XDG_CONFIG_HOME": str(home / ".config"),
        "XDG_CACHE_HOME": str(home / ".cache"),
        "XDG_DATA_HOME": str(home / ".local/share"),
        "XDG_STATE_HOME": str(home / ".local/state"),
        "TMPDIR": str(home), "TMP": str(home), "TEMP": str(home),
        "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8",
    }


def cleanup(process):
    if process.stdin:
        with contextlib.suppress(Exception):
            process.stdin.close()
    if process.poll() is not None:
        return
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=3)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=3)


class Fixture(http.server.ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, name):
        super().__init__(("127.0.0.1", 0), Handler)
        self.name = name
        self.url = "http://127.0.0.1:%d" % self.server_port
        self.lock = threading.Lock()
        self.handshakes = []
        self.requests = []
        self.errors = []
        self.connections = set()
        self.counter = 0
        self.request_bytes = 0
        self.thread = threading.Thread(target=self.serve_forever, daemon=True)
        self.thread.start()

    def error(self, message):
        with self.lock:
            if len(self.errors) < 100:
                self.errors.append(str(message)[:500])

    def record_handshake(self, transport, header):
        with self.lock:
            need(len(self.handshakes) < 100, "handshake count limit")
            self.handshakes.append({"transport": transport,
                                    "attestation": header})

    def events(self, request):
        need(isinstance(request, dict), "Responses request must be an object")
        need(request.get("model") == MODEL, "model routing changed")
        with self.lock:
            need(len(self.requests) < 100, "Responses request count limit")
            self.request_bytes += len(encode(request))
            need(self.request_bytes <= 32 * 1024 * 1024,
                 "aggregate Responses data limit")
            self.counter += 1
            serial = self.counter
            self.requests.append(copy.deepcopy(request))
        rid = "%s-response-%d" % (self.name, serial)
        result = [{"type": "response.created", "response": {"id": rid}}]
        if request.get("generate") is not False:
            result.append({
                "type": "response.output_item.done",
                "item": {
                    "type": "message", "role": "assistant",
                    "id": "%s-message-%d" % (self.name, serial),
                    "content": [{"type": "output_text",
                                 "text": "fixture-answer-" + self.name}],
                },
            })
        result.append({
            "type": "response.completed",
            "response": {
                "id": rid,
                "usage": {
                    "input_tokens": 0, "input_tokens_details": None,
                    "output_tokens": 0, "output_tokens_details": None,
                    "total_tokens": 0,
                },
            },
        })
        return result

    def close(self):
        self.shutdown()
        with self.lock:
            connections = list(self.connections)
        for connection in connections:
            with contextlib.suppress(OSError):
                connection.shutdown(socket.SHUT_RDWR)
            with contextlib.suppress(OSError):
                connection.close()
        self.server_close()
        self.thread.join(timeout=2)


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def setup(self):
        super().setup()
        self.connection.settimeout(TIMEOUT)
        with self.server.lock:
            self.server.connections.add(self.connection)

    def finish(self):
        try:
            super().finish()
        finally:
            with self.server.lock:
                self.server.connections.discard(self.connection)

    def reply(self, value, status=200):
        data = encode(value)
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
        self.wfile.flush()

    def do_GET(self):
        try:
            path = self.path.split("?", 1)[0]
            if self.headers.get("Upgrade", "").lower() == "websocket":
                need(path == "/v1/responses", "unexpected WebSocket route " + path)
                return self.websocket()
            if path == "/backend-api/wham/config/bundle":
                return self.reply({})
            if path == "/backend-api/wham/settings/user":
                return self.reply({"commit_attribution_enabled": False})
            if path in ("/backend-api/wham/accounts/check",
                        "/api/codex/accounts/check"):
                return self.reply({
                    "accounts": [{
                        "id": self.headers.get("chatgpt-account-id", "account-123"),
                        "workspace_backend_origin": self.server.url,
                        "account_routing_override": "NO_CONSTRAINT",
                    }],
                })
            self.server.error("unexpected GET " + path)
            self.reply({"error": "unexpected fixture route"}, 404)
        except (EOFError, BrokenPipeError, ConnectionResetError):
            pass
        except Exception as exc:
            self.server.error(type(exc).__name__ + ": " + str(exc))
            self.close_connection = True

    def do_POST(self):
        try:
            path = self.path.split("?", 1)[0]
            need(path == "/v1/responses", "unexpected POST route " + path)
            size = int(self.headers.get("Content-Length", "-1"))
            need(0 <= size <= MAX, "unbounded HTTP request")
            request = json.loads(self.exact(size))
            self.server.record_handshake(
                "sse", self.headers.get("x-oai-attestation"))
            events = self.server.events(request)
            data = b"".join(
                b"data: " + encode(event) + b"\n\n" for event in events)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            self.wfile.flush()
        except Exception as exc:
            self.server.error(type(exc).__name__ + ": " + str(exc))
            self.close_connection = True

    def exact(self, count):
        data = self.rfile.read(count)
        if len(data) != count:
            raise EOFError("short frame")
        return data

    def send_frame(self, opcode, data):
        need(len(data) <= MAX, "outbound frame too large")
        header = bytes([0x80 | opcode])
        if len(data) < 126:
            header += bytes([len(data)])
        elif len(data) <= 65535:
            header += b"\x7e" + struct.pack("!H", len(data))
        else:
            header += b"\x7f" + struct.pack("!Q", len(data))
        self.wfile.write(header + data)
        self.wfile.flush()

    def read_frame(self):
        a, b = self.exact(2)
        need(a & 0x80, "fragmented WS frame unsupported by fixture")
        need(not (a & 0x70), "unexpected WS extension")
        need(b & 0x80, "client WS frame must be masked")
        size = b & 127
        if size == 126:
            size = struct.unpack("!H", self.exact(2))[0]
        elif size == 127:
            size = struct.unpack("!Q", self.exact(8))[0]
        need(size <= MAX, "inbound frame too large")
        mask = self.exact(4)
        data = self.exact(size)
        return a & 15, bytes(x ^ mask[i % 4] for i, x in enumerate(data))

    def websocket(self):
        key = self.headers.get("Sec-WebSocket-Key")
        need(key is not None, "missing WS key")
        accept = base64.b64encode(hashlib.sha1(
            (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()
        ).digest()).decode()
        self.server.record_handshake(
            "websocket", self.headers.get("x-oai-attestation"))
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.end_headers()
        self.wfile.flush()
        self.close_connection = True
        deadline = time.monotonic() + 120
        for _ in range(100):
            need(time.monotonic() < deadline, "WS connection deadline")
            opcode, data = self.read_frame()
            if opcode == 8:
                self.send_frame(8, data)
                return
            if opcode == 9:
                self.send_frame(10, data)
                continue
            if opcode == 10:
                continue
            need(opcode == 1, "unexpected WS opcode")
            request = json.loads(data)
            need(request.get("type") == "response.create",
                 "unexpected WS message type")
            for event in self.server.events(request):
                self.send_frame(1, encode(event))
        raise AssertionError("too many WS frames")


class Peer:
    def __init__(self, command, env, workspace, attestation):
        self.process = subprocess.Popen(
            command, cwd=workspace, env=env,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, start_new_session=True, bufsize=0,
        )
        PEERS.append(self)
        self.attestation = attestation
        self.attestation_count = 0
        self.messages = queue.Queue(maxsize=10000)
        self.saved = []
        self.write_lock = threading.Lock()
        self.next_id = 0
        self.stderr = bytearray()
        self.reader_error = None
        self.closing = False
        self.reader = threading.Thread(target=self.read_stdout, daemon=True)
        self.diagnostics = threading.Thread(target=self.read_stderr, daemon=True)
        self.reader.start()
        self.diagnostics.start()

    def send(self, value):
        data = encode(value) + b"\n"
        need(len(data) <= MAX, "outgoing JSONL limit")
        with self.write_lock:
            # FileIO writes can be short. Loop until the complete frame is sent.
            view = memoryview(data)
            while view:
                count = self.process.stdin.write(view)
                need(count is not None and count > 0, "short pipe write")
                view = view[count:]

    def read_stderr(self):
        while True:
            data = self.process.stderr.read(4096)
            if not data:
                return
            self.stderr.extend(data)
            if len(self.stderr) > 65536:
                del self.stderr[:-65536]

    def read_stdout(self):
        try:
            while True:
                # Unbuffered FileIO.readline still reads through the newline.
                # A dedicated reader services the official 100 ms RPC deadline.
                line = self.process.stdout.readline(MAX + 1)
                if not line:
                    if self.closing:
                        return
                    raise RuntimeError("process stdout EOF")
                need(len(line) <= MAX, "incoming JSONL limit")
                msg = json.loads(line)
                if "id" in msg and "method" in msg:
                    need(msg["method"] == "attestation/generate",
                         "unexpected server RPC " + msg["method"])
                    self.attestation_count += 1
                    need(self.attestation != "forbidden",
                         "bridge leaked front-end attestation request")
                    need(msg.get("params") == {}, "attestation params changed")
                    if self.attestation == "failure":
                        self.send({
                            "id": msg["id"],
                            "error": {"code": -32601,
                                      "message": "synthetic unavailable control"},
                        })
                    else:
                        self.send({"id": msg["id"], "result": {"token": MARKER}})
                    continue
                self.messages.put_nowait(msg)
        except Exception as exc:
            self.reader_error = exc
            with contextlib.suppress(queue.Full):
                self.messages.put_nowait(exc)

    def wait(self, predicate):
        deadline = time.monotonic() + TIMEOUT
        while True:
            if self.reader_error:
                raise self.reader_error
            for index, msg in enumerate(self.saved):
                if predicate(msg):
                    return self.saved.pop(index)
            remaining = deadline - time.monotonic()
            need(remaining > 0, "RPC/notification deadline")
            try:
                msg = self.messages.get(timeout=remaining)
            except queue.Empty:
                raise AssertionError("RPC/notification deadline")
            if isinstance(msg, Exception):
                raise msg
            if predicate(msg):
                return msg
            self.saved.append(msg)
            need(len(self.saved) < 10000, "notification buffer limit")

    def rpc(self, method, params):
        self.next_id += 1
        ident = self.next_id
        self.send({"id": ident, "method": method, "params": params})
        result = self.wait(lambda m: m.get("id") == ident and
                           "method" not in m)
        need("error" not in result,
             method + " returned error " + json.dumps(result.get("error")))
        need("result" in result, method + " missing result")
        return result["result"]

    def initialize(self, gateway=False):
        result = self.rpc("initialize", {
            "clientInfo": {
                "name": "codex_desktop", "title": "Synthetic isolated CI",
                "version": "0.1.0",
            },
            "capabilities": {
                "experimentalApi": True, "requestAttestation": True,
                "explicitGatewayOauth": gateway,
            },
        })
        self.send({"method": "initialized", "params": {}})
        return result

    def turn(self, thread_id, text, endpoint):
        result = self.rpc("turn/start", {
            "threadId": thread_id,
            "input": [{"type": "text", "text": text, "text_elements": []}],
        })
        turn_id = result["turn"]["id"]
        done = self.wait(lambda m:
            m.get("method") == "turn/completed"
            and m.get("params", {}).get("threadId") == thread_id
            and m.get("params", {}).get("turn", {}).get("id") == turn_id)
        turn = done["params"]["turn"]
        need(turn.get("status") == "completed",
             "turn did not complete: " + json.dumps(turn))
        need(turn.get("error") is None, "completed turn carried an error")
        answer = self.wait(lambda m:
            m.get("method") == "item/completed"
            and m.get("params", {}).get("threadId") == thread_id
            and m.get("params", {}).get("turnId") == turn_id
            and m.get("params", {}).get("item", {}).get("type")
                == "agentMessage")
        need(answer["params"]["item"].get("text") ==
             "fixture-answer-" + endpoint, "answer crossed provider route")
        return turn_id

    def close(self):
        # Successful validation includes normal EOF shutdown and exit status 0.
        # Forced termination is only allowed by cleanup after a failed test.
        self.closing = True
        self.process.stdin.close()
        try:
            result = self.process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            raise AssertionError("normal shutdown did not exit within 10 seconds")
        self.reader.join(timeout=2)
        self.diagnostics.join(timeout=2)
        need(not self.reader.is_alive(), "stdout reader did not stop")
        need(not self.diagnostics.is_alive(), "stderr reader did not stop")
        need(result == 0, "normal shutdown exit status %d" % result)
        if self.reader_error:
            raise self.reader_error


def homes(root, name, endpoint, provider, catalog):
    home = root / (name + "-home")
    codex_home = root / (name + "-codex")
    home.mkdir(mode=0o700)
    codex_home.mkdir(mode=0o700)
    jwt = ".".join([
        b64(encode({"alg": "none", "typ": "JWT"})),
        b64(encode({"https://api.openai.com/auth":
                    {"chatgpt_plan_type": "pro"}})),
        b64(b"signature"),
    ])
    auth = {
        "auth_mode": "chatgpt", "OPENAI_API_KEY": None,
        "tokens": {
            "id_token": jwt, "access_token": "access-chatgpt",
            "refresh_token": "refresh-token", "account_id": None,
        },
        "last_refresh": datetime.datetime.now(datetime.timezone.utc)
            .isoformat().replace("+00:00", "Z"),
    }
    (codex_home / "auth.json").write_bytes(encode(auth))
    (codex_home / "auth.json").chmod(0o600)
    config = f'''
model = "{MODEL}"
model_provider = "{provider}"
model_catalog_json = "{catalog}"
cli_auth_credentials_store = "file"
approval_policy = "never"
sandbox_mode = "read-only"
chatgpt_base_url = "{endpoint.url}/backend-api"
check_for_update_on_startup = false

[analytics]
enabled = false

[cloud.skills]
enabled = false

# This narrow runtime fixture does not exercise optional Apps or plugins.
# These are stable release settings, not hidden test hooks or policy overrides.
[features]
apps = false
plugins = false

[model_providers.{provider}]
name = "Synthetic {provider}"
base_url = "{endpoint.url}/v1"
wire_api = "responses"
request_max_retries = 0
stream_max_retries = 0
requires_openai_auth = true
supports_websockets = true
'''
    (codex_home / "config.toml").write_text(config)
    return home, codex_home


def verify_fixture(endpoint, expected, prompts):
    REPORT.setdefault("observations", []).append({
        "endpoint": endpoint.name, "handshakes": endpoint.handshakes,
        "request_count": len(endpoint.requests), "fixture_errors": endpoint.errors,
    })
    need(not endpoint.errors, "fixture errors: " + json.dumps(endpoint.errors))
    need(endpoint.handshakes, "no Responses connection observed")
    need(any(h["transport"] == "websocket" for h in endpoint.handshakes),
         "no actual WebSocket handshake observed")
    for handshake in endpoint.handshakes:
        raw = handshake["attestation"]
        need(raw is not None, "missing attestation header")
        need(json.loads(raw) == expected,
             "unexpected official attestation envelope: " + raw)
    actual = json.dumps(endpoint.requests)
    for prompt in prompts:
        need(prompt in actual, "prompt absent from selected provider")
    need(endpoint.requests, "no actual Responses requests")
    return {
        "endpoint": endpoint.name,
        "connections": len(endpoint.handshakes),
        "requests": len(endpoint.requests),
        "transports": sorted(set(h["transport"]
                                 for h in endpoint.handshakes)),
        "attestation": expected,
    }


def direct_control(root, workspace, catalog, mode):
    endpoint = Fixture("direct-" + mode)
    peer = None
    try:
        home, ch = homes(root, "direct-" + mode, endpoint,
                         "direct_provider", catalog)
        peer = Peer([CODEX, "app-server"], clean_env(home, ch),
                    workspace, mode)
        peer.initialize()
        models = peer.rpc("model/list", {})
        need(any(m.get("model") == MODEL for m in models["data"]),
             "official model/list missing selected model")
        thread = peer.rpc("thread/start", {
            "model": MODEL,
            "environments": [{"environmentId": "local",
                              "cwd": str(workspace)}],
        })["thread"]
        prompt = "synthetic-direct-" + mode
        peer.turn(thread["id"], prompt, endpoint.name)
        need(peer.attestation_count > 0,
             "direct control never requested attestation")
        peer.close()
        expected = {"v": 1, "s": 2} if mode == "failure" else {
            "v": 1, "s": 0, "t": MARKER}
        result = verify_fixture(endpoint, expected, [prompt])
        result["case"] = "direct-" + mode
        REPORT["checks"].append(result)
    finally:
        if peer:
            cleanup(peer.process)
        endpoint.close()


def bridge_case(root, workspace, catalog):
    endpoints = [Fixture("alpha"), Fixture("beta")]
    peer = None
    try:
        backends = []
        for endpoint in endpoints:
            provider = endpoint.name + "_provider"
            home, ch = homes(root, "bridge-" + endpoint.name, endpoint,
                             provider, catalog)
            backends.append({
                "id": endpoint.name, "provider": provider,
                "command": CODEX, "home": str(home), "codex_home": str(ch),
                "startup_probe_sha256": digest(CODEX),
            })
        config = {
            "backends": backends,
            "working_directory": str(workspace),
            "attestation": "unavailable-v0.166",
            "gateway_oauth": "primary-read-only-v0.166",
            "gateway_oauth_provider_name": "Synthetic alpha_provider",
            "config_requirements_read": "compare-safe-v0.166",
        }
        config_path = root / "bridge.json"
        config_path.write_bytes(encode(config))
        front_home = root / "frontend-home"
        front_codex = root / "frontend-codex"
        front_home.mkdir()
        front_codex.mkdir()
        env = clean_env(front_home, front_codex)
        env["MAGPIE_APP_SERVER_BRIDGE_CONFIG"] = str(config_path)
        peer = Peer([BRIDGE, "app-server"], env, workspace, "forbidden")
        peer.initialize(gateway=True)
        # No injected descriptors: observe and route the actual official lists.
        models = peer.rpc("model/list", {})
        available = {m["model"] for m in models["data"]}
        selected = ["magpie/alpha/" + MODEL, "magpie/beta/" + MODEL]
        need(set(selected) <= available, "model/list missing provider aliases")
        requirements = peer.rpc("configRequirements/read", {})
        need(requirements == {
            "requirements": None, "supportsIndependentSpeedModes": True,
        }, "unexpected real requirements result: " + json.dumps(requirements))
        REPORT["requirements_read"] = requirements
        gateway = peer.rpc("account/gatewayOAuth/read", {})
        need(gateway == {
            "providerId": "alpha_provider",
            "providerName": "Synthetic alpha_provider",
            "required": False, "status": None, "error": None,
        }, "unexpected no-OAuth gateway result: " + json.dumps(gateway))
        REPORT["gateway_read"] = gateway
        for index, endpoint in enumerate(endpoints):
            started = peer.rpc("thread/start", {
                "model": selected[index],
                "environments": [{"environmentId": "local",
                                  "cwd": str(workspace)}],
            })
            thread = started["thread"]
            need(thread.get("modelProvider") ==
                 backends[index]["provider"], "thread provider changed")
            turns = []
            for number in (1, 2):
                prompt = "synthetic-%s-turn-%d" % (endpoint.name, number)
                turns.append(peer.turn(thread["id"], prompt, endpoint.name))
            need(turns[0] != turns[1], "reused turn ID")
            REPORT["checks"].append({
                "case": "bridge-two-turns", "endpoint": endpoint.name,
                "thread_id": thread["id"], "turn_ids": turns,
            })
        need(peer.attestation_count == 0,
             "front-end received an attestation request")
        peer.close()
        for endpoint in endpoints:
            prompts = ["synthetic-%s-turn-%d" % (endpoint.name, n)
                       for n in (1, 2)]
            result = verify_fixture(endpoint, {"v": 1, "s": 2}, prompts)
            other = "beta" if endpoint.name == "alpha" else "alpha"
            need("synthetic-" + other + "-turn-" not in
                 json.dumps(endpoint.requests), "cross-provider prompt leakage")
            result["case"] = "bridge-envelope-and-routing"
            REPORT["checks"].append(result)
    finally:
        if peer:
            cleanup(peer.process)
        for endpoint in endpoints:
            endpoint.close()


def main():
    need(sys.platform.startswith("linux"), "Linux container required")
    need(os.geteuid() != 0, "non-root container required")
    need({name for _, name in socket.if_nameindex()} == {"lo"},
         "network isolation missing: expected only loopback")
    for path in (CODEX, BRIDGE, "/inputs/models.json"):
        need(pathlib.Path(path).is_file(), "missing input " + path)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix="magpie-official-", dir="/tmp") as td:
        root = pathlib.Path(td)
        workspace = root / "workspace"
        workspace.mkdir()
        catalog = json.loads(pathlib.Path("/inputs/models.json").read_text())
        need(isinstance(catalog.get("models"), list) and catalog["models"],
             "invalid pinned model catalog")
        selected = [m for m in catalog["models"] if m.get("slug") == MODEL]
        need(len(selected) == 1, "pinned catalog must contain one gpt-5.5")
        model = selected[0]
        need(model.get("visibility") == "list"
             and model.get("tool_mode") is None
             and model.get("use_responses_lite") is False
             and model.get("prefer_websockets") is True,
             "pinned gpt-5.5 fixture assumptions changed")
        # Exact source record; no capability, identity, or display-name changes.
        catalog_path = root / "models.json"
        catalog_path.write_bytes(encode({"models": [model]}))
        version_home = root / "version-home"
        version_codex = root / "version-codex"
        version_home.mkdir()
        version_codex.mkdir()
        result = subprocess.run(
            [CODEX, "--version"], env=clean_env(version_home, version_codex),
            cwd=workspace, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            timeout=10, check=True,
        )
        version = result.stdout.decode().strip()
        need(version == "codex-cli " + VERSION,
             "unexpected official version: " + version)
        REPORT["official_version"] = version
        REPORT["official_binary_sha256"] = digest(CODEX)
        REPORT["bridge_binary_sha256"] = digest(BRIDGE)
        REPORT["stage"] = "direct-rpc-failure"
        direct_control(root, workspace, catalog_path, "failure")
        REPORT["stage"] = "direct-synthetic-marker"
        direct_control(root, workspace, catalog_path, "marker")
        REPORT["stage"] = "two-backend-bridge"
        bridge_case(root, workspace, catalog_path)


if __name__ == "__main__":
    def deadline(_signum, _frame):
        raise TimeoutError("whole-harness deadline")
    signal.signal(signal.SIGALRM, deadline)
    signal.alarm(240)
    try:
        main()
        REPORT["status"] = "passed"
    except Exception as exc:
        REPORT["status"] = "failed"
        REPORT["failure"] = {
            "type": type(exc).__name__, "message": str(exc)[:2000],
        }
        # All children have fresh synthetic-only homes and no inherited secrets.
        REPORT["synthetic_child_stderr_tails"] = [
            p.stderr[-4096:].decode("utf-8", errors="replace") for p in PEERS
        ]
    finally:
        signal.alarm(0)
        for peer in reversed(PEERS):
            with contextlib.suppress(Exception):
                cleanup(peer.process)
    print(json.dumps(REPORT, indent=2, sort_keys=True))
    sys.exit(0 if REPORT["status"] == "passed" else 1)
