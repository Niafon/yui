"""Create a local, offline model configuration and a private LAN TLS CA."""
import datetime as dt
import ipaddress
import json
from pathlib import Path
import shutil
import socket
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID

ROOT = Path(__file__).resolve().parents[1]
tls_dir = ROOT / 'data' / 'tls'
tls_dir.mkdir(parents=True, exist_ok=True)
addresses = {'127.0.0.1', '::1'}
for entry in socket.getaddrinfo(socket.gethostname(), None):
    addr = entry[4][0].split('%')[0]
    if not ipaddress.ip_address(addr).is_link_local:
        addresses.add(addr)
names = ['localhost', socket.gethostname()]
now = dt.datetime.now(dt.timezone.utc)
ca_path = tls_dir / 'yui-ca.crt'
ca_key_path = tls_dir / 'yui-ca.key'
if ca_path.exists():
    ca = x509.load_pem_x509_certificate(ca_path.read_bytes())
    ca_key = serialization.load_pem_private_key(ca_key_path.read_bytes(), password=None)
else:
    ca_key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'Yui Local Command Center CA')])
    ca = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(ca_key.public_key())
          .serial_number(x509.random_serial_number()).not_valid_before(now-dt.timedelta(minutes=5))
          .not_valid_after(now+dt.timedelta(days=3650))
          .add_extension(x509.BasicConstraints(ca=True,path_length=0),critical=True)
          .add_extension(x509.KeyUsage(False,False,False,False,False,True,True,False,False),critical=True)
          .sign(ca_key,hashes.SHA256()))
    ca_path.write_bytes(ca.public_bytes(serialization.Encoding.PEM))
    ca_key_path.write_bytes(ca_key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
key = ec.generate_private_key(ec.SECP256R1())
cert = (x509.CertificateBuilder().subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'Yui Command Center')]))
        .issuer_name(ca.subject).public_key(key.public_key()).serial_number(x509.random_serial_number())
        .not_valid_before(now-dt.timedelta(minutes=5)).not_valid_after(now+dt.timedelta(days=365))
        .add_extension(x509.BasicConstraints(ca=False,path_length=None),critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]),critical=False)
        .add_extension(x509.SubjectAlternativeName([x509.DNSName(n) for n in names]+[x509.IPAddress(ipaddress.ip_address(a)) for a in sorted(addresses)]),critical=False)
        .sign(ca_key,hashes.SHA256()))
(tls_dir/'server.crt').write_bytes(cert.public_bytes(serialization.Encoding.PEM))
(tls_dir/'server.key').write_bytes(key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
(ROOT/'desktop/public').mkdir(parents=True,exist_ok=True)
shutil.copyfile(ca_path,ROOT/'desktop/public/yui-ca.crt')
(tls_dir/'addresses.json').write_text(json.dumps(sorted(addresses)),encoding='utf-8')

config_path = ROOT/'yui.config.json'
if not config_path.exists():
    cfg = json.loads((ROOT/'yui.config.example.json').read_text(encoding='utf-8'))
    cfg['server'].update(addr='0.0.0.0:8765',local_addr='127.0.0.1:8766',stage_dir='desktop/dist',tls_cert_file='data/tls/server.crt',tls_key_file='data/tls/server.key',require_tls_for_remote=True)
    keep = {'qwen35-9b-gpu','qwen35-9b-cpu','qwen35-4b-cpu','embed-worker','stt-worker','tts-worker','vision-llm'}
    cfg['providers'] = [p for p in cfg['providers'] if p['id'] in keep]
    for p in cfg['providers']:
        p.pop('//',None)
        if p['id'] == 'stt-worker':
            p.update(model='faster-whisper-small',model_family='whisper-small',worker_id='stt')
        if p['id'] == 'tts-worker':
            p.update(model='piper-irina-ru',model_family='piper-irina',execution_backend='cpu',estimated_vram_mb=0,estimated_ram_mb=250,gaming_safe=True,worker_id='tts')
        if p['id'] == 'embed-worker':
            p.update(estimated_ram_mb=2800,worker_id='embeddings')
        if p['id'] == 'vision-llm':
            p.update(endpoint='http://127.0.0.1:11435/v1',model='qwen3.5-9b',worker_id='llm-qwen9-gpu',execution_backend='gpu',auto_select=True,estimated_vram_mb=7200)
    cfg['default_providers']['llm'] = 'qwen35-9b-gpu'
    cfg['workers'] = [w for w in cfg['workers'] if w['id'] != 'vision']
    python = str(ROOT/'.venv/Scripts/python.exe')
    llama = str(ROOT/'bin/llama/llama-server.exe')
    worker_models = {'embeddings':ROOT/'models/Qwen3-Embedding-0.6B','stt':ROOT/'models/faster-whisper-small','tts':ROOT/'models/piper/ru/ru_RU/irina/medium/ru_RU-irina-medium.onnx'}
    for w in cfg['workers']:
        w.pop('//',None)
        if w['id'].startswith('llm-'):
            w['command'] = llama
            w['dir'] = str(ROOT/'core')
            w['args'][1] = str(ROOT/'models'/Path(w['args'][1]).name)
            w['args'] += ['--jinja','--reasoning-budget','0','--parallel','1']
            if w['id'] == 'llm-qwen9-gpu':
                w['args'] += ['--mmproj',str(ROOT/'models/mmproj-F16.gguf'),'--no-mmproj-offload']
        else:
            w['command'] = python
            w['dir'] = str(ROOT/'workers')
            w['args'][w['args'].index('--model')+1] = str(worker_models[w['id']])
            w['env'] = {'HF_HUB_OFFLINE':'1','TRANSFORMERS_OFFLINE':'1','HF_HOME':str(ROOT/'models/.cache'),'OMP_NUM_THREADS':'4','MKL_NUM_THREADS':'4','PYTHONUTF8':'1'}
    cfg['inference'].update(vram_reserve_mb=1536,warmup_timeout_seconds=180)
    config_path.write_text(json.dumps(cfg,ensure_ascii=False,indent=2),encoding='utf-8')
    print('Created yui.config.json with local models only.')
else:
    print('Preserved existing yui.config.json; renewed LAN server certificate.')
print('LAN addresses:', ', '.join(a for a in sorted(addresses) if ':' not in a and not a.startswith('127.')))
print('CA SHA256:',ca.fingerprint(hashes.SHA256()).hex())
