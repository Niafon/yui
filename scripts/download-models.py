"""Download pinned local models/runtime, resume partial files and verify SHA-256."""
import concurrent.futures
import hashlib
import json
from pathlib import Path
import time
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]

def get_json(url):
    with urllib.request.urlopen(url, timeout=60) as response:
        return json.load(response)

def hf(repo, folder, select):
    meta = get_json(f"https://huggingface.co/api/models/{repo}?blobs=true")
    result = []
    for item in meta['siblings']:
        name = item['rfilename']
        if select(name):
            result.append(dict(url=f"https://huggingface.co/{repo}/resolve/{meta['sha']}/{name}",
                path=str(Path('models') / folder / name), size=item.get('size'),
                sha256=item.get('lfs', {}).get('sha256'), repo=repo, revision=meta['sha']))
    return result

def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def download(item):
    path = ROOT / item['path']
    path.parent.mkdir(parents=True, exist_ok=True)
    expected = item.get('sha256')
    if path.exists() and (not item.get('size') or path.stat().st_size == item['size']):
        if not expected or digest(path) == expected:
            print(f"Verified {path.name}", flush=True)
            return
    partial = path.with_name(path.name + '.partial')
    for attempt in range(5):
        try:
            offset = partial.stat().st_size if partial.exists() else 0
            if offset and offset == item.get('size'):
                pass
            else:
                request = urllib.request.Request(item['url'], headers={'Range': f'bytes={offset}-'} if offset else {})
                with urllib.request.urlopen(request, timeout=120) as response:
                    append = offset > 0 and response.status == 206
                    with partial.open('ab' if append else 'wb') as stream:
                        while chunk := response.read(4 * 1024 * 1024):
                            stream.write(chunk)
            if item.get('size') and partial.stat().st_size != item['size']:
                raise ValueError(f"Incomplete download: {path.name}")
            if expected and digest(partial) != expected:
                partial.unlink()
                raise ValueError(f"Checksum mismatch: {path.name}")
            partial.replace(path)
            print(f"Installed {item['path']}", flush=True)
            return
        except Exception as exc:
            print(f"Retry {attempt+1}: {path.name}: {exc}", flush=True)
            if attempt == 4:
                raise
            time.sleep(2 * (attempt+1))

def main():
    manifest_path = ROOT / 'models' / 'installed-manifest.json'
    if manifest_path.exists():
        items = json.loads(manifest_path.read_text())
    else:
        items = hf('unsloth/Qwen3.5-9B-GGUF', '', lambda n: n in ['Qwen3.5-9B-Q4_K_M.gguf', 'mmproj-F16.gguf'])
        items += hf('unsloth/Qwen3.5-4B-GGUF', '', lambda n: n == 'Qwen3.5-4B-Q4_K_M.gguf')
        items += hf('Qwen/Qwen3-Embedding-0.6B', 'Qwen3-Embedding-0.6B', lambda n: n.endswith(('.json', '.safetensors', '.txt', '.md')) and not n.startswith(('onnx/', 'openvino/')))
        items += hf('Systran/faster-whisper-small', 'faster-whisper-small', lambda n: n.endswith(('.json', '.bin', '.txt', '.md')))
        items += hf('rhasspy/piper-voices', 'piper', lambda n: n.startswith('ru/ru_RU/irina/medium/') and n.endswith(('.onnx', '.json', '.md', 'MODEL_CARD')))
        release = get_json('https://api.github.com/repos/ggml-org/llama.cpp/releases/tags/b10819')
        for asset in release['assets']:
            if asset['name'] in ['llama-b10819-bin-win-cuda-12.4-x64.zip', 'cudart-llama-bin-win-cuda-12.4-x64.zip']:
                items.append(dict(url=asset['browser_download_url'], path='.cache/'+asset['name'], size=asset['size'], sha256=asset['digest'].split(':')[1]))
        manifest_path.write_text(json.dumps(items, indent=2), encoding='utf-8')
    print(f"Downloading/verifying {len(items)} files, {sum(i.get('size',0) for i in items)/1e9:.2f} GB", flush=True)
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        list(pool.map(download, items))
    for item in items:
        if item['path'].endswith('.zip'):
            with zipfile.ZipFile(ROOT / item['path']) as archive:
                archive.extractall(ROOT / 'bin' / 'llama')
    print('All model files verified; llama.cpp extracted.', flush=True)

if __name__ == '__main__':
    main()
