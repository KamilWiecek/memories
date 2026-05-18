#!/usr/bin/env python3
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

from faster_whisper import WhisperModel

model_name = os.environ.get("WHISPER_MODEL", "base")
print(f"Loading model: {model_name}", flush=True)
model = WhisperModel(model_name, device="cpu", compute_type="int8")
print("Model ready", flush=True)

_server = None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        if self.path == "/shutdown":
            self.send_response(200)
            self.end_headers()
            threading.Thread(target=_server.shutdown, daemon=True).start()
            return

        if self.path == "/transcribe":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length))
            audio_path = body.get("audio_path", "")
            try:
                segments, _ = model.transcribe(audio_path, language="pl")
                transcript = " ".join(s.text.strip() for s in segments)
                resp = json.dumps({"transcript": transcript}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(resp)))
                self.end_headers()
                self.wfile.write(resp)
            except Exception as exc:
                resp = json.dumps({"error": str(exc)}).encode()
                self.send_response(500)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(resp)))
                self.end_headers()
                self.wfile.write(resp)
        else:
            self.send_response(404)
            self.end_headers()


if __name__ == "__main__":
    _server = HTTPServer(("0.0.0.0", 9000), Handler)
    print("Whisper server listening on :9000", flush=True)
    _server.serve_forever()
    print("Shutdown complete", flush=True)