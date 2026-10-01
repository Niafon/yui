"""Exercise installed STT, TTS and embeddings locally, never accepting placeholders."""
import base64
import json
import math
import os
from pathlib import Path
import sys
import time
import wave

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'workers'))
os.environ.update(HF_HUB_OFFLINE='1',TRANSFORMERS_OFFLINE='1',OMP_NUM_THREADS='4',MKL_NUM_THREADS='4')
from yui_worker.workers.tts import TTSWorker
from yui_worker.workers.stt import STTWorker
from yui_worker.workers.embeddings import EmbeddingsWorker

report = {}
out = ROOT/'artifacts'
out.mkdir(exist_ok=True)
tts = TTSWorker(str(ROOT/'models/piper/ru/ru_RU/irina/medium/ru_RU-irina-medium.onnx'))
tts.load()
assert tts.backend == 'piper', tts.detail
started = time.monotonic()
reply = tts.handle({'text':'Привет. Это проверка звука. Компьютер готов к работе.','speed':1})
pcm = b''.join(base64.b64decode(c['audio_b64']) for c in reply['chunks'])
assert len(pcm) > 16000 and reply['sample_rate'] > 16000
with wave.open(str(out/'yui-voice-check.wav'),'wb') as wav:
    wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(reply['sample_rate']); wav.writeframes(pcm)
report['tts'] = {'backend':tts.detail,'sample_rate':reply['sample_rate'],'seconds':round(time.monotonic()-started,2)}
print('TTS:',report['tts'],flush=True)
stt = STTWorker(str(ROOT/'models/faster-whisper-small'),'cpu')
stt.load()
assert stt.model is not None,stt.detail
started = time.monotonic()
transcript = stt.handle({'audio_b64':base64.b64encode(pcm).decode(),'sample_rate':reply['sample_rate'],'channels':1,'format':'pcm16','language':'ru','final':True})
assert 'компьютер' in transcript['text'].lower(),transcript
report['stt'] = {'backend':stt.detail,'transcript':transcript['text'],'seconds':round(time.monotonic()-started,2)}
print('STT:',report['stt'],flush=True)
embed = EmbeddingsWorker(str(ROOT/'models/Qwen3-Embedding-0.6B'),256,'cpu')
embed.load()
assert embed.model is not None,embed.detail
started = time.monotonic()
vectors = embed.handle({'texts':['Компьютер управляет голосовым помощником.','Голосовой помощник работает на компьютере.'],'purpose':'document'})
assert vectors['dim'] == 256 and len(vectors['vectors']) == 2
for vec in vectors['vectors']:
    assert len(vec)==256 and all(math.isfinite(v) for v in vec)
    assert abs(sum(v*v for v in vec)-1) < .01
report['embeddings'] = {'backend':embed.detail,'dimension':256,'seconds':round(time.monotonic()-started,2)}
print('Embeddings:',report['embeddings'],flush=True)
(out/'model-check.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
