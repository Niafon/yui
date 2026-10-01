"""End to end checks over the real HTTP contract the core speaks."""

import sys, pathlib
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))

import base64
import json
import struct
import unittest
import urllib.error
import urllib.request

from yui_worker.base import serve
from yui_worker.workers.embeddings import EmbeddingsWorker
from yui_worker.workers.stt import STTWorker, rms_level
from yui_worker.workers.tts import TTSWorker
from yui_worker.workers.vision import VisionWorker, image_size


def post(url, payload):
    req = urllib.request.Request(
        url, data=json.dumps(payload).encode(), headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        return json.loads(resp.read())


def get(url):
    with urllib.request.urlopen(url, timeout=10) as resp:
        return resp.status, json.loads(resp.read())


class WorkerCase(unittest.TestCase):
    worker_factory = None

    def setUp(self):
        self.worker = self.worker_factory()
        self.httpd = serve(self.worker, "127.0.0.1", 0)
        host, port = self.httpd.server_address[:2]
        self.base = f"http://{host}:{port}"

    def tearDown(self):
        self.httpd.shutdown()
        self.httpd.server_close()


class TestEmbeddings(WorkerCase):
    worker_factory = EmbeddingsWorker

    def test_health_reports_backend(self):
        status, body = get(self.base + "/healthz")
        self.assertEqual(status, 200)
        self.assertTrue(body["ready"])
        self.assertEqual(body["kind"], "embeddings")

    def test_embed_returns_normalised_vectors(self):
        body = post(self.base + "/v1/embeddings", {"texts": ["чай", "кофе"]})
        self.assertEqual(len(body["vectors"]), 2)
        self.assertEqual(body["dim"], len(body["vectors"][0]))

    def test_missing_field_is_a_client_error(self):
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            post(self.base + "/v1/embeddings", {})
        self.assertEqual(ctx.exception.code, 400)


class TestSTT(WorkerCase):
    worker_factory = STTWorker

    def test_silence_produces_no_text(self):
        silence = base64.b64encode(b"\x00\x00" * 16000).decode()
        body = post(self.base + "/v1/stt", {"audio_b64": silence, "sample_rate": 16000, "final": True})
        self.assertEqual(body["text"], "")

    def test_loud_audio_produces_placeholder_text(self):
        pcm = b"".join(struct.pack("<h", 12000 if i % 2 else -12000) for i in range(16000))
        body = post(
            self.base + "/v1/stt",
            {"audio_b64": base64.b64encode(pcm).decode(), "sample_rate": 16000, "final": True},
        )
        self.assertIn("placeholder", body["text"])
        self.assertTrue(body["final"])

    def test_rms_level(self):
        self.assertEqual(rms_level(b"\x00\x00" * 100), 0.0)
        self.assertGreater(rms_level(struct.pack("<h", 20000) * 100), 0.5)


class TestTTS(WorkerCase):
    worker_factory = TTSWorker

    def test_returns_ordered_chunks(self):
        body = post(self.base + "/v1/tts", {"text": "Привет, это проверка синтеза речи."})
        self.assertEqual(body["sample_rate"], 16000)
        self.assertGreater(len(body["chunks"]), 1)
        self.assertEqual([c["seq"] for c in body["chunks"]], list(range(len(body["chunks"]))))
        first = base64.b64decode(body["chunks"][0]["audio_b64"])
        self.assertGreater(len(first), 0)

    def test_empty_text_is_rejected(self):
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            post(self.base + "/v1/tts", {"text": "   "})
        self.assertEqual(ctx.exception.code, 400)

    def test_emotion_changes_the_audio(self):
        neutral = post(self.base + "/v1/tts", {"text": "одно и то же предложение", "emotion": "neutral"})
        joyful = post(self.base + "/v1/tts", {"text": "одно и то же предложение", "emotion": "joy"})
        self.assertNotEqual(neutral["chunks"][0]["audio_b64"], joyful["chunks"][0]["audio_b64"])


class TestVision(WorkerCase):
    worker_factory = VisionWorker

    def test_reads_png_dimensions(self):
        png = (
            b"\x89PNG\r\n\x1a\n" + b"\x00\x00\x00\rIHDR" + struct.pack(">II", 640, 480) + b"\x08\x02\x00\x00\x00"
        )
        self.assertEqual(image_size(png), (640, 480))
        body = post(self.base + "/v1/vision", {"image_b64": base64.b64encode(png).decode(), "mime": "image/png"})
        self.assertIn("640x480", body["description"])

    def test_question_is_echoed_into_the_observation(self):
        png = b"\x89PNG\r\n\x1a\n" + b"\x00\x00\x00\rIHDR" + struct.pack(">II", 10, 10) + b"\x08\x02\x00\x00\x00"
        body = post(
            self.base + "/v1/vision",
            {"image_b64": base64.b64encode(png).decode(), "question": "что это?"},
        )
        self.assertIn("что это?", body["description"])

    def test_unknown_operation_is_404(self):
        with self.assertRaises(urllib.error.HTTPError) as ctx:
            post(self.base + "/v1/nope", {})
        self.assertEqual(ctx.exception.code, 404)


if __name__ == "__main__":
    unittest.main()
