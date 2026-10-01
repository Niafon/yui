"""Worker HTTP host.

Uses only the standard library so a worker starts even before the ML
dependencies are installed: the core can then report an honest "model not
loaded" instead of failing silently (NFR-010).
"""

from __future__ import annotations

import json
import logging
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable, Dict, Optional

from .protocol import ProtocolError

log = logging.getLogger("yui.worker")

Handler = Callable[[Dict[str, Any]], Dict[str, Any]]


class Worker:
    """A worker exposes one or more operations under /v1/<op>."""

    def __init__(self, worker_id: str, kind: str) -> None:
        self.worker_id = worker_id
        self.kind = kind
        self.ops: Dict[str, Handler] = {}
        self.ready = False
        self.detail = "starting"

    def op(self, name: str) -> Callable[[Handler], Handler]:
        def register(fn: Handler) -> Handler:
            self.ops[name] = fn
            return fn

        return register

    def load(self) -> None:
        """Load models. Override in subclasses; must set self.ready."""
        self.ready = True
        self.detail = "ready"

    def health(self) -> Dict[str, Any]:
        return {
            "id": self.worker_id,
            "kind": self.kind,
            "ready": self.ready,
            "detail": self.detail,
        }


def serve(worker: Worker, host: str = "127.0.0.1", port: int = 0) -> ThreadingHTTPServer:
    """Start the worker HTTP server and return it (already serving)."""

    class RequestHandler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args: Any) -> None:  # quieter default
            log.debug("%s - %s", self.address_string(), fmt % args)

        def _send(self, code: int, body: Dict[str, Any]) -> None:
            raw = json.dumps(body, ensure_ascii=False).encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self) -> None:  # noqa: N802 - required by BaseHTTPRequestHandler
            if self.path.rstrip("/") == "/healthz":
                self._send(200 if worker.ready else 503, worker.health())
                return
            self._send(404, {"error": "not found"})

        def do_POST(self) -> None:  # noqa: N802
            if not self.path.startswith("/v1/"):
                self._send(404, {"error": "not found"})
                return
            op = self.path[len("/v1/"):].strip("/")
            handler: Optional[Handler] = worker.ops.get(op)
            if handler is None:
                self._send(404, {"error": f"unknown operation: {op}"})
                return
            try:
                length = int(self.headers.get("Content-Length") or 0)
                payload = json.loads(self.rfile.read(length) or b"{}")
                if not isinstance(payload, dict):
                    raise ProtocolError("payload must be an object")
                self._send(200, handler(payload))
            except ProtocolError as exc:
                self._send(400, {"error": str(exc)})
            except Exception as exc:  # noqa: BLE001 - report, do not crash the worker
                log.exception("operation %s failed", op)
                self._send(500, {"error": str(exc).strip() or f"{type(exc).__name__} during {op}"})

    httpd = ThreadingHTTPServer((host, port), RequestHandler)
    httpd.daemon_threads = True
    worker.load()
    thread = threading.Thread(target=httpd.serve_forever, name=f"yui-{worker.worker_id}", daemon=True)
    thread.start()
    log.info("worker %s listening on http://%s:%d", worker.worker_id, *httpd.server_address[:2])
    return httpd
