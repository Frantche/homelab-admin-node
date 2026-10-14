#!/usr/bin/env python3
import http.server
import json
import os
import subprocess
import tempfile
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class MockState:
    def __init__(self):
        self.vault = None
        self.tokens = {}
        self.token_names = {}
        self.next_id = 1
        self.fail_vault_write = False


def handler_for(state):
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def send_json(self, status, payload):
            body = json.dumps(payload).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/api/v1/users/admin/tokens":
                self.send_json(200, [{"id": token_id, "name": name} for token_id, name in state.token_names.items()])
                return
            if self.path == "/v1/homelab/data/gitea/runner":
                if state.vault is None:
                    self.send_json(404, {"errors": ["not found"]})
                else:
                    self.send_json(200, {"data": {"data": state.vault}})
                return
            self.send_json(404, {})

        def do_POST(self):
            payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path == "/api/v1/users/admin/tokens":
                token_id = state.next_id
                state.next_id += 1
                token = f"test-garm-token-{token_id}"
                state.tokens[token_id] = token
                state.token_names[token_id] = payload["name"]
                self.send_json(201, {"id": token_id, "name": payload["name"], "sha1": token})
                return
            if self.path == "/v1/homelab/data/gitea/runner":
                if state.fail_vault_write:
                    self.send_json(500, {"errors": ["simulated write failure"]})
                    return
                state.vault = payload["data"]
                self.send_json(200, {"data": {"version": 1}})
                return
            self.send_json(404, {})

        def do_DELETE(self):
            prefix = "/api/v1/users/admin/tokens/"
            if self.path.startswith(prefix):
                state.tokens.pop(int(self.path[len(prefix):]), None)
                state.token_names.pop(int(self.path[len(prefix):]), None)
                self.send_response(204)
                self.end_headers()
                return
            self.send_json(404, {})

    return Handler


def run_contract(port, rotation_id):
    vars_data = {
        "gitea_api_validate_certs": False,
        "gitea_config_url": f"http://127.0.0.1:{port}",
        "gitea_runtime": {"admin_user": "admin", "admin_password": "ci-admin-password"},
        "gitea_config": {"garm_token": {
            "enabled": True,
            "name": "garm-token",
            "scopes": ["write:repository", "write:organization"],
            "rotation_id": rotation_id,
            "openbao": {"mount": "homelab", "path": "gitea/runner"},
        }},
        "openbao": {"root_token": "ci-openbao-token"},
        "openbao_config": {
            "secret_engines": [{"path": "homelab", "type": "kv-v2"}],
            "validate_certs": False,
        },
        "openbao_config_url": f"http://127.0.0.1:{port}",
        "service_domains": {"openbao": "127.0.0.1"},
        "ci_mode": True,
    }
    with tempfile.NamedTemporaryFile("w", encoding="utf-8", delete=False) as stream:
        json.dump(vars_data, stream)
        vars_path = stream.name
    try:
        result = subprocess.run(
            [
                "ansible-playbook",
                "-i",
                "localhost,",
                str(ROOT / "ci/playbooks/gitea-garm-token-contracts.yml"),
                "-e",
                f"@{vars_path}",
            ],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            env={**os.environ, "ANSIBLE_NOCOLOR": "1", "ANSIBLE_ROLES_PATH": str(ROOT / "ansible/roles")},
            check=False,
        )
        return result.returncode, result.stdout
    finally:
        os.unlink(vars_path)


def require(condition, message):
    if not condition:
        raise SystemExit(message)


def main():
    state = MockState()
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler_for(state))
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    port = server.server_address[1]
    try:
        code, output = run_contract(port, "v1")
        require(code == 0, f"initial GARM token provision failed:\n{output}")
        require(state.vault["token"] == "test-garm-token-1", "first token was not published")
        require(state.vault["token_id"] == 1 and state.tokens.get(1), "first token metadata missing")
        require("test-garm-token-1" not in output, "token appeared in Ansible output")

        code, output = run_contract(port, "v1")
        require(code == 0, f"idempotent GARM token convergence failed:\n{output}")
        require(state.next_id == 2, "convergence created another token without rotation")

        code, output = run_contract(port, "v2")
        require(code == 0, f"GARM token rotation failed:\n{output}")
        require(state.vault["token"] == "test-garm-token-2", "replacement token was not published")
        require(1 not in state.tokens and 2 in state.tokens, "previous token was not revoked")

        state.fail_vault_write = True
        code, output = run_contract(port, "v3")
        require(code != 0, "simulated OpenBao write failure unexpectedly succeeded")
        require(state.vault["token"] == "test-garm-token-2", "failed rotation changed the saved token")
        require(
            2 in state.tokens and 3 not in state.tokens,
            "failed rotation did not preserve old token and revoke new token",
        )
        require("test-garm-token-3" not in output, "failed token appeared in Ansible output")
    finally:
        server.shutdown()
        thread.join(timeout=5)
        server.server_close()
    print("Gitea GARM token contracts passed")


if __name__ == "__main__":
    main()
