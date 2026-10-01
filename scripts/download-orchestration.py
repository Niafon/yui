"""Download verified, revision-pinned optional models. Never changes live config."""
import argparse
import concurrent.futures
import importlib.util
import json
from pathlib import Path
import platform
import zipfile

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("downloads", ROOT / "scripts/download-models.py")
downloads = importlib.util.module_from_spec(spec)
spec.loader.exec_module(downloads)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--fara", action="store_true", help="include community Fara GGUF and projector")
    parser.add_argument("--runtime", choices=["none", "cpu", "cuda", "vulkan"], default="none")
    parser.add_argument("--plan", action="store_true")
    args = parser.parse_args()
    manifest = ROOT / "models/orchestration-manifest.json"
    selection = {"fara": args.fara, "runtime": args.runtime}
    cached = json.loads(manifest.read_text()) if manifest.exists() else {}
    if cached.get("selection") == selection:
        items = cached["files"]
    else:
        items = downloads.hf("LiquidAI/LFM2.5-2.6B-GGUF", "", lambda n: n == "LFM2.5-2.6B-Q4_K_M.gguf")
        items += downloads.hf("prism-ml/Ternary-Bonsai-2-27B-gguf", "", lambda n: n == "Ternary-Bonsai-2-27B-PTQ1_0.gguf")
        if args.fara:
            items += downloads.hf("bartowski/Fara1.5-4B-GGUF", "", lambda n: n in ("Fara1.5-4B-Q4_K_M.gguf", "mmproj-Fara1.5-4B-f16.gguf"))
        expected = 4 if args.fara else 2
        if len(items) != expected or any(not i.get("sha256") for i in items):
            raise RuntimeError("model catalog incomplete or lacks checksums")
        if args.runtime != "none":
            if platform.system() != "Windows" or platform.machine().lower() not in ("amd64", "x86_64"):
                raise RuntimeError("Automatic runtime archive installation supports Windows x64; supply a PrismML runtime on other hosts")
            release = downloads.get_json("https://api.github.com/repos/PrismML-Eng/llama.cpp/releases/latest")
            suffix = {"cpu": "cpu-x64.zip", "cuda": "cuda-12.4-x64.zip", "vulkan": "vulkan-x64.zip"}[args.runtime]
            assets = [a for a in release["assets"] if a["name"].endswith("win-" + suffix)]
            if len(assets) != (2 if args.runtime == "cuda" else 1):
                raise RuntimeError("expected runtime assets missing")
            for a in assets:
                if not a.get("digest", "").startswith("sha256:"):
                    raise RuntimeError("runtime checksum unavailable")
                items.append(dict(url=a["browser_download_url"], path=".cache/"+a["name"], size=a["size"], sha256=a["digest"].split(":")[1]))
        manifest.parent.mkdir(exist_ok=True)
        manifest.write_text(json.dumps({"selection": selection, "files": items}, indent=2))
    print(f"Verified plan: {len(items)} files, {sum(i.get('size', 0) for i in items)/1e9:.2f} GB", flush=True)
    if args.plan:
        return
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        list(pool.map(downloads.download, items))
    target = (ROOT / "bin/prism-llama").resolve()
    for item in items:
        if not item["path"].endswith(".zip"):
            continue
        with zipfile.ZipFile(ROOT/item["path"]) as archive:
            for info in archive.infolist():
                if not (target / info.filename).resolve().is_relative_to(target):
                    raise RuntimeError("unsafe runtime archive path")
            archive.extractall(target)
    print("Models verified. Existing runtime and configuration preserved.")


if __name__ == "__main__":
    main()
