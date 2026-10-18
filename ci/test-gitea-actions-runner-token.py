#!/usr/bin/env python3
import base64
import http.server
import json
import os
import subprocess
import tempfile
import threading
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TOKEN_PATH = "/v1/secret/data/ci/gitea/instance-runner"
GITEA_TOKEN_PATH = "/api/v1/admin/actions/runners/registration-token"


class MockState:
    def __init__(self):
        self.vault = None
        self.runner_token = "test-instance-runner-token-1"
        self.token_requests = 0
        self.vault_writes = 0
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
            if self.path == TOKEN_PATH:
                if state.vault is None:
                    self.send_json(404, {"errors": ["not found"]})
                else:
                    self.send_json(200, {"data": {"data": state.vault}})
                return
            self.send_json(404, {})

        def do_POST(self):
            if self.path == GITEA_TOKEN_PATH:
                expected = "Basic " + base64.b64encode(b"admin:ci-admin-password").decode()
                if self.headers.get("Authorization") != expected:
                    self.send_json(401, {"message": "basic auth required"})
                    return
                state.token_requests += 1
                self.send_json(200, {"token": state.runner_token})
                return
            if self.path == TOKEN_PATH:
                payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if state.fail_vault_write:
                    self.send_json(500, {"errors": ["simulated write failure"]})
                    return
                state.vault = payload["data"]
                state.vault_writes += 1
                self.send_json(200, {"data": {"version": state.vault_writes}})
                return
            self.send_json(404, {})

    return Handler


def run_contract(port):
    vars_data = {
        "gitea_api_validate_certs": False,
        "gitea_config_url": f"http://127.0.0.1:{port}",
        "gitea_runtime": {"admin_user": "admin", "admin_password": "ci-admin-password"},
        "gitea_config": {
            "actions_runner_registration_token": {
                "enabled": True,
                "openbao": {"mount": "secret", "path": "ci/gitea/instance-runner"},
            }
        },
        "openbao": {"root_token": "ci-openbao-token"},
        "openbao_config": {
            "secret_engines": [{"path": "secret", "type": "kv-v2"}],
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
                str(ROOT / "ci/playbooks/gitea-actions-runner-token-contracts.yml"),
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
        code, output = run_contract(port)
        require(code == 0, f"initial instance runner token publication failed:\n{output}")
        require(state.vault == {"token": "test-instance-runner-token-1"}, "instance token was not published")
        require(state.vault_writes == 1, "initial token was not written exactly once")
        require("test-instance-runner-token-1" not in output, "registration token appeared in Ansible output")

        code, output = run_contract(port)
        require(code == 0, f"idempotent instance runner token convergence failed:\n{output}")
        require(state.vault_writes == 1, "unchanged token was written to OpenBao again")

        state.runner_token = "test-instance-runner-token-2"
        code, output = run_contract(port)
        require(code == 0, f"rotated instance runner token sync failed:\n{output}")
        require(state.vault == {"token": "test-instance-runner-token-2"}, "rotated token was not published")
        require(state.vault_writes == 2, "rotated token was not written exactly once")

        state.runner_token = "test-instance-runner-token-3"
        state.fail_vault_write = True
        code, output = run_contract(port)
        require(code != 0, "simulated OpenBao write failure unexpectedly succeeded")
        require(state.vault == {"token": "test-instance-runner-token-2"}, "failed write changed the saved token")
        require("test-instance-runner-token-3" not in output, "failed token appeared in Ansible output")
    finally:
        server.shutdown()
        thread.join(timeout=5)
        server.server_close()
    print("Gitea instance runner registration token contracts passed")


if __name__ == "__main__":
    main()
