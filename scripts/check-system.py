"""HTTPS, device pairing, revocation and real inference smoke check."""
import base64
import json
from pathlib import Path
import ssl
import struct
import time
import urllib.error
import urllib.request
import zlib

ROOT = Path(__file__).resolve().parents[1]
token = json.loads((ROOT/'.cache/check-runtime.json').read_text())['token']
ctx = ssl.create_default_context(cafile=str(ROOT/'data/tls/yui-ca.crt'))
report = {}

def request(path, body=None, auth=token, host='localhost', method=None):
    headers = {'Content-Type':'application/json'}
    if auth: headers['Authorization'] = 'Bearer '+auth
    req = urllib.request.Request(f'https://{host}:8765'+path, data=None if body is None else json.dumps(body).encode(),headers=headers,method=method)
    # Direct LAN access must not go through a system HTTP proxy.
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=ctx))
    with opener.open(req,timeout=240) as response:
        return json.load(response)

for port in (8801,8802,8803):
    deadline = time.monotonic()+180
    while True:
        try:
            with urllib.request.urlopen(f'http://127.0.0.1:{port}/healthz',timeout=3) as response: health=json.load(response)
            assert health['ready'] and 'fallback' not in health.get('detail','') and 'placeholder' not in health.get('detail',''),health
            report[str(port)] = health
            break
        except Exception:
            if time.monotonic()>deadline: raise
            time.sleep(1)
print('All workers ready with real local backends',flush=True)
session = request('/v1/sessions',{})
started=time.monotonic()
answer=request(f"/v1/sessions/{session['id']}/message",{'text':'Привет! Это проверка запуска. Ответь по-русски одним коротким предложением, что ты готова.'})
report['chat']={'result':answer,'seconds':round(time.monotonic()-started,2)}
print('Chat:',json.dumps(report['chat'],ensure_ascii=False),flush=True)
assert answer.get('reply'),answer

def chunk(kind,data): return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
png=b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',64,64,8,2,0,0,0))+chunk(b'IDAT',zlib.compress((b'\0'+b'\xff\x00\x00'*64)*64))+chunk(b'IEND',b'')
started=time.monotonic()
vision=request(f"/v1/sessions/{session['id']}/vision",{'image_b64':base64.b64encode(png).decode(),'mime':'image/png','question':'Какого цвета изображение? Ответь кратко по-русски.'})
assert vision.get('description'),vision
report['vision']={'result':vision,'seconds':round(time.monotonic()-started,2)}
print('Vision:',json.dumps(report['vision'],ensure_ascii=False),flush=True)
lan = next(a for a in json.loads((ROOT/'data/tls/addresses.json').read_text()) if a.startswith('192.168.'))
pair=request('/v1/pair/start',{})
device=request('/v1/pair/claim',{'code':pair['code'],'kind':'web','name':'Integration check'},auth=None,host=lan)
status=request('/v1/status',auth=device['token'],host=lan)
assert status
try:
    request('/v1/pair/claim',{'code':pair['code']},auth=None,host=lan)
    raise AssertionError('Pairing code replay accepted')
except urllib.error.HTTPError as exc: assert exc.code==403,exc.code
try:
    request('/v1/status',host=lan)
    raise AssertionError('Desktop token accepted remotely')
except urllib.error.HTTPError as exc: assert exc.code==401,exc.code
request(f"/v1/devices/{device['device_id']}/revoke",{})
try:
    request('/v1/status',auth=device['token'],host=lan)
    raise AssertionError('Revoked token accepted')
except urllib.error.HTTPError as exc: assert exc.code==401,exc.code
request(f"/v1/sessions/{session['id']}/close",{})
report['pairing']={'lan':lan,'tls_verified':True,'one_time_code':True,'desktop_token_rejected_remotely':True,'revocation':True}
(ROOT/'artifacts/system-check.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
print('HTTPS LAN pairing, replay protection and revocation passed',flush=True)
