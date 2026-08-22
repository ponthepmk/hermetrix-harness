#!/usr/bin/env python3
"""Deterministic localhost MCP 2026-07-28 fixture for manual/E2E QA."""

import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


PROTOCOL = "2026-07-28"


class Handler(BaseHTTPRequestHandler):
    server_version = "HermetrixMCPFixture/1.0"

    def log_message(self, format_string, *args):
        print(format_string % args, flush=True)

    def do_POST(self):
        try:
            size = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(size))
        except (ValueError, json.JSONDecodeError):
            self.send_error(400, "invalid JSON")
            return
        method = request.get("method", "")
        params = request.get("params") or {}
        meta = params.get("_meta") or {}
        if (
            self.headers.get("MCP-Protocol-Version") != PROTOCOL
            or self.headers.get("Mcp-Method") != method
            or meta.get("io.modelcontextprotocol/protocolVersion") != PROTOCOL
        ):
            self.send_error(400, "invalid MCP request metadata")
            return
        if method == "tools/list":
            result = {
                "resultType": "complete",
                "tools": [
                    {
                        "name": "fixture_echo",
                        "title": "Fixture Echo",
                        "description": "Return deterministic evidence from the Hermetrix E2E MCP fixture.",
                        "inputSchema": {
                            "type": "object",
                            "properties": {"text": {"type": "string"}},
                            "required": ["text"],
                            "additionalProperties": False,
                        },
                        "outputSchema": {
                            "type": "object",
                            "properties": {"echoed": {"type": "string"}},
                            "required": ["echoed"],
                            "additionalProperties": False,
                        },
                        "annotations": {"readOnlyHint": True, "openWorldHint": False},
                    }
                ],
            }
        elif method == "tools/call":
            if self.headers.get("Mcp-Name") != "fixture_echo":
                self.send_error(400, "invalid Mcp-Name")
                return
            text = (params.get("arguments") or {}).get("text", "")
            result = {
                "resultType": "complete",
                "content": [{"type": "text", "text": f"FIXTURE_OK:{text}"}],
                "structuredContent": {"echoed": text},
                "isError": False,
            }
        else:
            self.send_error(404, "unknown MCP method")
            return
        payload = json.dumps(
            {"jsonrpc": "2.0", "id": request.get("id"), "result": result},
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="127.0.0.1")
    parser.add_argument("--port", default=18444, type=int)
    args = parser.parse_args()
    if args.listen not in {"127.0.0.1", "::1", "localhost"}:
        raise SystemExit("fixture refuses non-loopback listeners")
    server = ThreadingHTTPServer((args.listen, args.port), Handler)
    print(f"MCP fixture listening on http://{args.listen}:{args.port}/mcp", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
