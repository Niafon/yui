import pathlib
import sys
import unittest
from unittest.mock import Mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
from yui_worker.workers.tts import TTSWorker
from yui_worker.base import Worker, serve
from test_workers import post
import urllib.error
import json


class TestTTSFragments(unittest.TestCase):
    def test_russian_voice_skips_non_speech_without_calling_model(self):
        worker = TTSWorker()
        worker.ready = True
        worker.backend = 'silero'
        worker.model = Mock()
        for text in ['...', '**', '😊', '&#x20;', 'OpenRouter', '123']:
            with self.subTest(text=text):
                self.assertEqual(worker.handle({'text': text})['chunks'], [])
        worker.model.apply_tts.assert_not_called()

    def test_empty_exception_has_useful_http_error(self):
        worker = Worker('test', 'tts')
        def fail(payload):
            raise ValueError()
        worker.op('tts')(fail)
        server = serve(worker, port=0)
        try:
            with self.assertRaises(urllib.error.HTTPError) as caught:
                post(f'http://127.0.0.1:{server.server_port}/v1/tts', {'text': 'test'})
            self.assertEqual(caught.exception.code, 500)
            self.assertEqual(json.load(caught.exception)['error'], 'ValueError during tts')
        finally:
            server.shutdown()
            server.server_close()
