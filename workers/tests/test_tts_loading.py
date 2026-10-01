import pathlib
import sys
import unittest
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[1]))
from yui_worker.workers.tts import TTSWorker


class TestVoiceLoading(unittest.TestCase):
    def test_missing_configured_voice_is_not_a_ready_tone(self):
        worker = TTSWorker('missing-voice.pt')
        worker.load()
        self.assertFalse(worker.ready)
        self.assertIn('voice unavailable', worker.detail)
        with self.assertRaises(RuntimeError):
            worker.handle({'text': 'Привет'})
