"""Sequential real-model smoke check, with independent ports and owned processes."""
import argparse
import json
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", type=Path, default=ROOT/"yui.orchestration.json")
    parser.add_argument("--models", nargs="+", default=["lfm25-cpu", "bonsai2-gpu", "fara15-gpu"])
    args = parser.parse_args()
    cfg = json.loads(args.config.read_text(encoding="utf-8"))
    report = []
    folder = ROOT / "artifacts/orchestration-2026-09-20"
    folder.mkdir(parents=True, exist_ok=True)
    for index, id in enumerate(args.models):
        provider = next(p for p in cfg["providers"] if p["id"] == id)
        worker = next(w for w in cfg["workers"] if w["id"] == provider["worker_id"])
        command = [worker["command"]] + worker["args"][:]
        port = 19438 + index
        command[command.index("--port")+1] = str(port)
        started = time.monotonic()
        with (folder / (id+".log")).open("w", encoding="utf-8") as log:
            proc = subprocess.Popen(command, cwd=worker["dir"], stdout=log, stderr=log,
                                    creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
            try:
                endpoint = f"http://127.0.0.1:{port}"
                deadline = time.monotonic()+180
                while True:
                    if proc.poll() is not None:
                        raise RuntimeError(f"{id} exited with {proc.returncode}; see model log")
                    try:
                        with urllib.request.urlopen(endpoint+"/health", timeout=2) as r:
                            if r.status == 200:
                                break
                    except (OSError, urllib.error.URLError):
                        pass
                    if time.monotonic() > deadline:
                        raise TimeoutError("startup timeout")
                    time.sleep(.5)
                load_seconds = time.monotonic()-started
                body = {"model": provider["model"], "stream": False, "max_tokens": 96,
                        "temperature": 0, "chat_template_kwargs": {"enable_thinking": False},
                        "messages": [{"role": "user", "content": "Reply with only the word OK."}]}
                call = urllib.request.Request(endpoint+"/v1/chat/completions", data=json.dumps(body).encode(), headers={"Content-Type":"application/json"})
                with urllib.request.urlopen(call, timeout=180) as r:
                    result = json.load(r)
                choice = result["choices"][0]["message"]
                if not choice.get("content") and not choice.get("reasoning_content"):
                    raise RuntimeError("empty generated response")
                report.append(dict(provider=id, status="ok", load_seconds=round(load_seconds, 2),
                                   total_seconds=round(time.monotonic()-started, 2), usage=result.get("usage")))
                print(json.dumps(report[-1]), flush=True)
            except Exception as exc:
                report.append(dict(provider=id,status="error",error=str(exc)))
                print(json.dumps(report[-1]),flush=True)
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill(); proc.wait(timeout=10)
    (folder/"model-smoke.json").write_text(json.dumps(report, indent=2))
    if any(r["status"] != "ok" for r in report):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
