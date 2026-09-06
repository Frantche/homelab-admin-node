#!/usr/bin/env python3
"""Stateful Harbor/OpenBao HTTP mock for pull-secret contract tests."""

from __future__ import annotations

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse


class State:
    robot: dict | None = None
    vault: dict | None = None
    creates = 0
    updates = 0
    refreshes = 0
    vault_writes = 0


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format: str, *_args: object) -> None:
        return

    def send_json(self, status: int, payload: object) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_json(self) -> dict:
        length = int(self.headers.get("Content-Length", "0"))
        return json.loads(self.rfile.read(length) or b"{}")

    def do_GET(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/v1/secret/data/shared/harbor/pull":
            if State.vault is None:
                self.send_json(404, {"errors": []})
            else:
                self.send_json(200, {"data": {"data": State.vault}})
        elif path == "/api/v2.0/robots":
            self.send_json(200, [] if State.robot is None else [State.robot])
        elif path == "/__state":
            self.send_json(
                200,
                {
                    "robot": State.robot,
                    "vault": State.vault,
                    "creates": State.creates,
                    "updates": State.updates,
                    "refreshes": State.refreshes,
                    "vault_writes": State.vault_writes,
                },
            )
        else:
            self.send_json(404, {"error": path})

    def do_POST(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        payload = self.read_json()
        if path == "/api/v2.0/robots":
            State.creates += 1
            State.robot = {
                "id": 42,
                "name": "robot$" + payload["name"],
                "description": payload["description"],
                "level": "system",
                "disable": False,
                "expires_at": -1,
                "permissions": payload["permissions"],
            }
            self.send_json(201, {**State.robot, "secret": "CreatedSecret123"})
        elif path == "/v1/secret/data/shared/harbor/pull":
            State.vault_writes += 1
            State.vault = payload["data"]
            self.send_json(200, {"data": {"version": State.vault_writes}})
        else:
            self.send_json(404, {"error": path})

    def do_PUT(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        if path == "/api/v2.0/robots/42":
            State.updates += 1
            payload = self.read_json()
            State.robot = {**State.robot, **payload}
            self.send_json(200, {})
        else:
            self.send_json(404, {"error": path})

    def do_PATCH(self) -> None:  # noqa: N802
        path = urlparse(self.path).path
        self.read_json()
        if path == "/api/v2.0/robots/42":
            State.refreshes += 1
            self.send_json(200, {"secret": f"RefreshedSecret{State.refreshes}x"})
        else:
            self.send_json(404, {"error": path})


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, required=True)
    args = parser.parse_args()
    ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
