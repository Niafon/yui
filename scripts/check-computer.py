"""Read-only real Fara integration smoke test against example.com."""
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.request

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'workers'))
from yui_worker.workers.computer import ComputerWorker


def main():
    cfg=json.loads((ROOT/'yui.orchestration.json').read_text(encoding='utf-8'))
    model=next(p for p in cfg['providers'] if p['id']=='fara15-gpu')
    spec=next(w for w in cfg['workers'] if w['id']==model['worker_id'])
    args=spec['args'][:]; args[args.index('--port')+1]='19441'
    folder=ROOT/'artifacts/orchestration-2026-09-20'
    with (folder/'computer-model.log').open('w') as log:
        proc=subprocess.Popen([spec['command']]+args,stdout=log,stderr=log,cwd=ROOT,
                              creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        worker=ComputerWorker(endpoint='http://127.0.0.1:19441/v1', max_rounds=3)
        job=None
        try:
            deadline=time.monotonic()+180
            while True:
                if proc.poll() is not None: raise RuntimeError('model exited')
                try:
                    with urllib.request.urlopen('http://127.0.0.1:19441/health',timeout=2): break
                except OSError: pass
                if time.monotonic()>deadline: raise TimeoutError('model startup')
                time.sleep(.5)
            worker.load()
            print(worker.health(),flush=True)
            job=worker.start(dict(identity_id='smoke-test', task='Read the main heading of the current page and report it. Do not click, type, or navigate. Then terminate.',
                                  start_url='https://example.com',allowed_hosts=['example.com']))
            deadline=time.monotonic()+180
            while job['status']=='running':
                if time.monotonic()>deadline: raise TimeoutError('Fara task')
                time.sleep(1)
                job=worker.dispatch(worker.status(dict(id=job['id'],identity_id='smoke-test')))
            (folder/'computer-smoke.json').write_text(json.dumps(job,ensure_ascii=False,indent=2),encoding='utf-8')
            print(json.dumps(job,ensure_ascii=False),flush=True)
            if job['status']!='complete' or 'example domain' not in job['answer'].lower(): raise RuntimeError('Fara did not complete the read-only task')
        finally:
            if job is not None:
                worker.dispatch(worker.cancel(dict(id=job['id'],identity_id='smoke-test')))
            proc.terminate()
            try: proc.wait(timeout=10)
            except subprocess.TimeoutExpired: proc.kill(); proc.wait(timeout=10)


if __name__=='__main__': main()
