import base64
import math
import struct
import sys
import types
import unittest
from unittest.mock import Mock, patch

from yui_worker.workers.stt import STTWorker
from yui_worker.protocol import ProtocolError


class STTQualityTests(unittest.TestCase):
    def payload(self, **kwargs):
        return dict(audio_b64=base64.b64encode(struct.pack('<h', 10000)*16000).decode(), **kwargs)

    def segment(self, text='Привет Юи', **kwargs):
        fields = dict(text=text, avg_logprob=-0.2, no_speech_prob=0.05, compression_ratio=1.2, start=0, end=1)
        fields.update(kwargs)
        return types.SimpleNamespace(**fields)

    def test_vad_and_no_cross_session_prompt(self):
        worker = STTWorker()
        worker.model = Mock()
        worker.model.transcribe.return_value = (iter([self.segment(), self.segment('noise', no_speech_prob=0.9)]), types.SimpleNamespace(language='ru', language_probability=1))
        result = worker.handle(self.payload())
        self.assertEqual(result['text'], 'Привет Юи')
        self.assertAlmostEqual(result['confidence'], math.exp(-0.2))
        options = worker.model.transcribe.call_args.kwargs
        self.assertTrue(options['vad_filter'])
        self.assertFalse(options['condition_on_previous_text'])
        self.assertEqual(options['language'], 'ru')
        self.assertEqual(options['beam_size'], 5)

    def test_silence_does_not_invoke_model(self):
        worker = STTWorker()
        worker.model = Mock()
        self.assertEqual(worker.handle({'audio_b64': base64.b64encode(bytes(32000)).decode()})['text'], '')
        worker.model.transcribe.assert_not_called()

    def test_invalid_input(self):
        for kwargs in ({'format': 'mp3'}, {'sample_rate': 1}, {'channels': 3}):
            with self.subTest(kwargs=kwargs), self.assertRaises(ProtocolError):
                STTWorker().handle(self.payload(**kwargs))

    def test_model_failure_is_not_placeholder(self):
        worker = STTWorker('missing-model')
        with patch.dict(sys.modules, {'faster_whisper': types.SimpleNamespace(WhisperModel=Mock(side_effect=RuntimeError('missing'))) }):
            worker.load()
        self.assertFalse(worker.ready)
        with self.assertRaises(RuntimeError):
            worker.handle(self.payload())

    def test_partial_uses_fast_decode_and_rejects_repetitions(self):
        worker = STTWorker()
        worker.model = Mock()
        worker.model.transcribe.return_value = ([self.segment('repeated', compression_ratio=5)], types.SimpleNamespace(language='ru'))
        result = worker.handle(self.payload(final=False))
        self.assertEqual(result['text'], '')
        self.assertEqual(result['confidence'], 0)
        self.assertEqual(worker.model.transcribe.call_args.kwargs['beam_size'], 1)


if __name__ == '__main__':
    unittest.main()
