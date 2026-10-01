"""Merge portable local model profiles; preserve privacy, identity and storage.

Default writes a separate config. --apply atomically activates it with a backup.
No key values are read, saved or printed.
"""
import argparse
import copy
import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]


def gpu_available():
    try:
        result = subprocess.run(["nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits"],
                                capture_output=True, text=True, timeout=5, check=True,
                                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        return any(int(n.strip()) >= 6000 for n in result.stdout.splitlines())
    except (OSError, ValueError, subprocess.SubprocessError):
        return False


def make_config(base, root, llama, prism, python, computer_python, gpu=False, computer=False):
    cfg = copy.deepcopy(base)
    computer = computer or cfg.get("computer", {}).get("enabled", False)
    providers, workers = [], []

    def add(id, filename, server, port, kind, family, role, backend, quality, ram, vram=0, projector=None):
        path = root / "models" / filename
        if not path.is_file() or not server.is_file():
            raise ValueError(f"Missing model/runtime: {path} or {server}")
        if projector and not (root / "models" / projector).is_file():
            raise ValueError(f"Missing vision projector: {projector}")
        worker = "model-" + id
        tags = [role] if role else []
        if backend == "gpu":
            tags.append("exclusive-gpu")
        providers.append(dict(id=id, kind=kind, driver="openai", endpoint=f"http://127.0.0.1:{port}/v1",
            model=family, model_family=family, local=True, execution_backend=backend,
            quality=quality, estimated_ram_mb=ram, estimated_vram_mb=vram, gaming_safe=backend == "cpu",
            auto_select=True, keep_warm=role == "lightweight", worker_id=worker, tags=tags))
        args = ["-m", str(path), "--alias", family, "--host", "127.0.0.1", "--port", str(port),
                "-ngl", "99" if backend == "gpu" else "0", "-c", "8192", "--parallel", "1", "--jinja", "-t", "4"]
        if role == "reasoning":
            args += ["--reasoning-budget", "1024"]
        if projector:
            args += ["--mmproj", str(root/"models"/projector), "--no-mmproj-offload"]
        workers.append(dict(id=worker, command=str(server), args=args, dir=str(root),
                            autostart=False, restart=False, health_url=f"http://127.0.0.1:{port}/health"))

    add("lfm25-cpu", "LFM2.5-2.6B-Q4_K_M.gguf", llama, 11438, "llm", "lfm2.5-2.6b", "lightweight", "cpu", 72, 2600)
    add("bonsai2-cpu", "Ternary-Bonsai-2-27B-PTQ1_0.gguf", prism, 11439, "llm", "bonsai2-27b", "reasoning", "cpu", 94, 9000)
    if gpu:
        add("bonsai2-gpu", "Ternary-Bonsai-2-27B-PTQ1_0.gguf", prism, 11440, "llm", "bonsai2-27b", "reasoning", "gpu", 94, 1600, 7300)
    if computer:
        if not computer_python.is_file():
            raise ValueError("Install workers/requirements-computer.txt in .venv-computer first")
        fara_id = "fara15-gpu" if gpu else "fara15-cpu"
        fara_backend = "gpu" if gpu else "cpu"
        fara_ram, fara_vram = (1800, 4800) if gpu else (5500, 0)
        add(fara_id, "Fara1.5-4B-Q4_K_M.gguf", llama, 11441, "computer", "microsoft/Fara1.5-4B", "", fara_backend, 80, fara_ram, fara_vram,
            projector="mmproj-Fara1.5-4B-f16.gguf")
        workers.append(dict(id="computer", command=str(computer_python), dir=str(root/"workers"),
            args=["-m", "yui_worker", "--kind", "computer", "--port", "8810"],
            health_url="http://127.0.0.1:8810/healthz", autostart=False, restart=False,
            env={"PYTHONUTF8": "1"}))
        cfg["computer"] = dict(enabled=True, endpoint="http://127.0.0.1:8810", worker_id="computer", model_worker_id="model-"+fara_id)
    owned = {"lfm25-cpu", "bonsai2-cpu", "bonsai2-gpu", "fara15-cpu", "fara15-gpu"}
    cfg["providers"] = [p for p in cfg.get("providers", []) if p["id"] not in owned] + providers
    worker_ids = {"model-"+id for id in owned} | {"computer"}
    cfg["workers"] = [w for w in cfg.get("workers", []) if w["id"] not in worker_ids] + workers
    # Existing GPU profiles participate in eviction, including the old shared
    # Qwen LLM/vision worker; no duplicate GPU residency on a 10 GB card.
    gpu_workers = {p.get("worker_id") for p in cfg["providers"] if p.get("estimated_vram_mb", 0) > 0 and p.get("worker_id")}
    for p in cfg["providers"]:
        if p.get("worker_id") in gpu_workers:
            p["tags"] = list(dict.fromkeys(p.get("tags", []) + ["exclusive-gpu"]))
            p["keep_warm"] = False
    for w in cfg["workers"]:
        if w["id"] in gpu_workers:
            w["autostart"] = False
    cfg.setdefault("default_providers", {})["llm"] = "lfm25-cpu"
    inf = cfg.setdefault("inference", {})
    inf.update(enabled=True, mode="auto", preferred_provider="", locked_model="", allow_auto_downgrade=True,
               minimum_quality=55, vram_reserve_mb=1536, warmup_timeout_seconds=180)
    inf["jev"] = dict(enabled=True, endpoint="https://openrouter.ai/api/alpha/decisions",
        model="~typesafe/jev-latest", api_key_env="OPENROUTER_API_KEY", timeout_ms=2500,
        min_confidence=0.8, allow_current_text=True)
    cfg.setdefault("memory", {}).update(context_tokens=1200, small_context_tokens=600, small_retrieval_limit=4)
    return cfg


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--config", type=Path, default=ROOT/"yui.config.json")
    p.add_argument("--output", type=Path, default=ROOT/"yui.orchestration.json")
    p.add_argument("--apply", action="store_true")
    p.add_argument("--backend", choices=["auto", "cpu", "gpu"], default="auto",
                   help="auto detects NVIDIA; explicitly select gpu for Metal/Vulkan/ROCm runtimes")
    p.add_argument("--computer", action="store_true")
    exe = ".exe" if os.name == "nt" else ""
    p.add_argument("--llama-server", type=Path, default=ROOT/f"bin/llama/llama-server{exe}")
    p.add_argument("--bonsai-server", type=Path, default=ROOT/f"bin/prism-llama/llama-server{exe}")
    p.add_argument("--computer-python", type=Path, default=ROOT/(".venv-computer/Scripts/python.exe" if os.name == "nt" else ".venv-computer/bin/python"))
    args = p.parse_args()
    cfg = make_config(json.loads(args.config.read_text(encoding="utf-8-sig")), ROOT,
                      args.llama_server.resolve(), args.bonsai_server.resolve(), Path(sys.executable), args.computer_python.resolve(),
                      args.backend == "gpu" or (args.backend == "auto" and gpu_available()), args.computer)
    target = args.config if args.apply else args.output
    if target.exists():
        stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
        shutil.copy2(target, target.with_name(target.name+".before-orchestration-"+stamp))
    staged = target.with_suffix(target.suffix+".tmp")
    staged.write_text(json.dumps(cfg, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
    staged.replace(target)
    print(f"Wrote {target}; Jev uses an environment key and existing permission policy.")


if __name__ == "__main__":
    main()
