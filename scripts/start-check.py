"""Start the installed command center without a console for local verification."""
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import urllib.request

root = Path(__file__).resolve().parents[1]
with socket.socket() as probe:
    if probe.connect_ex(('127.0.0.1',8765)) == 0:
        raise SystemExit('Port 8765 is already in use; preserve existing process.')
state = root/'.cache/check-runtime.json'
token = json.loads(state.read_text())['token'] if state.exists() else secrets.token_urlsafe(32)
env = {**os.environ,'YUI_LOOPBACK_TOKEN':token,'PYTHONUTF8':'1'}
log = open(root/'.cache/core-check.log','w',encoding='utf-8')
process = subprocess.Popen([str(root/'bin/yui-core.exe'),'-config',str(root/'yui.config.json'),'-print-token=false'],cwd=root,env=env,stdout=log,stderr=log,creationflags=subprocess.CREATE_NO_WINDOW)
(root/'.cache/check-runtime.json').write_text(json.dumps({'pid':process.pid,'token':token}),encoding='utf-8')
print('Command center PID:',process.pid)
deadline = time.monotonic()+180
for port,health_path in [(8766,'/healthz'),(8801,'/healthz'),(8802,'/healthz'),(8803,'/healthz')]:
    while True:
        try:
            with urllib.request.urlopen(f'http://127.0.0.1:{port}{health_path}',timeout=2) as response:
                if response.status == 200: break
        except Exception:
            if process.poll() is not None or time.monotonic()>deadline: raise
            time.sleep(.5)
print('Command center and workers ready.')
