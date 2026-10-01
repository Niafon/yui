"""Optional Fara 1.5 bridge using Microsoft's harness and an ephemeral browser.

Runs are bounded, cancellable, identity-scoped and pause at critical points.
No user browser profile, desktop input API or shell tool is exposed.
"""
from __future__ import annotations

import asyncio
import importlib.util
import ipaddress
import tempfile
import threading
import uuid
from pathlib import Path
from urllib.parse import urlsplit

from ..base import Worker
from ..protocol import ProtocolError


def allowed_url(url: str, hosts: set[str]) -> bool:
    parsed = urlsplit(url)
    host = (parsed.hostname or "").lower().rstrip(".")
    if parsed.scheme not in ("https", "http") or parsed.username or parsed.password or host not in hosts:
        return False
    if host == "localhost" or host.endswith((".localhost", ".local", ".internal")):
        return False
    try:
        return ipaddress.ip_address(host).is_global
    except ValueError:
        return "." in host and "*" not in host


class ComputerWorker(Worker):
    def __init__(self, endpoint="http://127.0.0.1:11441/v1", model_name="microsoft/Fara1.5-4B", max_rounds=20):
        super().__init__("computer", "computer")
        self.endpoint, self.model_name = endpoint, model_name
        self.max_rounds = max(1, min(50, max_rounds))
        self.loop = asyncio.new_event_loop()
        self.jobs = {}
        self.op("computer/start")(self.start)
        self.op("computer/status")(lambda p: self.dispatch(self.status(p)))
        self.op("computer/reply")(lambda p: self.dispatch(self.reply(p)))
        self.op("computer/cancel")(lambda p: self.dispatch(self.cancel(p)))

    def load(self):
        url = urlsplit(self.endpoint)
        if url.scheme != "http" or url.hostname not in ("127.0.0.1", "::1", "localhost"):
            self.detail = "Fara requires a local model endpoint"
            return
        if importlib.util.find_spec("fara") is None:
            self.detail = "Microsoft Fara harness is not installed (requirements-computer.txt)"
            return
        threading.Thread(target=self.loop.run_forever, name="yui-computer", daemon=True).start()
        self.ready, self.detail = True, "Fara 1.5 official harness; ephemeral browser"

    def dispatch(self, coroutine):
        if not self.ready:
            coroutine.close()
            raise RuntimeError(self.detail)
        return asyncio.run_coroutine_threadsafe(coroutine, self.loop).result(timeout=15)

    def start(self, payload):
        return self.dispatch(self.begin(payload))

    async def begin(self, p):
        owner, task = p.get("identity_id"), p.get("task")
        hosts = p.get("allowed_hosts")
        if not isinstance(owner, str) or not owner or not isinstance(task, str) or not task.strip() or len(task) > 8000:
            raise ProtocolError("identity_id and a task of 1–8000 characters required")
        if not isinstance(hosts, list) or not hosts or len(hosts) > 30 or not all(isinstance(h, str) for h in hosts):
            raise ProtocolError("explicit allowed_hosts list required")
        hosts = {h.lower().rstrip(".") for h in hosts}
        if not all(allowed_url("https://"+h, hosts) for h in hosts):
            raise ProtocolError("allowed_hosts must contain public hostnames without schemes or wildcards")
        url = p.get("start_url")
        if not isinstance(url, str) or not allowed_url(url, hosts):
            raise ProtocolError("start_url must belong to allowed_hosts")
        if any(not j["future"].done() for j in self.jobs.values()):
            raise ProtocolError("another browser task is active; finish or cancel it first")
        self.jobs = {}  # completed results are bounded to the most recent task
        job_id = uuid.uuid4().hex
        job = dict(id=job_id, owner=owner, status="running", answer="", queue=asyncio.Queue())
        self.jobs[job_id] = job
        job["future"] = asyncio.create_task(self.run(job, task, hosts, url))
        return self.public(job)

    def lookup(self, p):
        job = self.jobs.get(p.get("id"))
        if job is None or job["owner"] != p.get("identity_id"):
            raise ProtocolError("unknown browser task")
        return job

    def public(self, job):
        return {k: job[k] for k in ("id", "status", "answer")}

    async def status(self, p):
        return self.public(self.lookup(p))

    async def reply(self, p):
        job = self.lookup(p)
        text = p.get("text")
        if job["status"] != "waiting_for_user" or not isinstance(text, str) or not text.strip() or len(text) > 8000:
            raise ProtocolError("task must be waiting for an explicit user response")
        job["status"] = "running"
        await job["queue"].put(text)
        return self.public(job)

    async def cancel(self, p):
        job = self.lookup(p)
        job["future"].cancel()
        try:
            await job["future"]
        except asyncio.CancelledError:
            pass
        job["status"] = "cancelled"
        return self.public(job)

    async def run(self, job, task, hosts, url):
        env = agent = run_context = None
        try:
            from fara.agents.fara.fara15_agent import Fara15Agent, Fara15AgentConfig
            from fara.environments.playwright import PlaywrightEnvironment
            from fara.core.run_context import RunContext
            from fara.core.data_point import Task, SolverStatus, UserMessage, UserMessageType

            class RestrictedBrowser(PlaywrightEnvironment):
                async def _setup_browser(self):
                    async def route(request_route):
                        if allowed_url(request_route.request.url, hosts):
                            await request_route.continue_()
                        else:
                            await request_route.abort()
                    await self._context.route("**/*", route)
                    self._context.on("page", lambda page: page.on("download", lambda download: asyncio.create_task(download.cancel())))
                    self._page.on("download", lambda download: asyncio.create_task(download.cancel()))
                    await super()._setup_browser()

            # Temporary traces disappear when the task ends; no screenshots or
            # credentials are copied to long-term memory automatically.
            with tempfile.TemporaryDirectory(prefix="yui-fara-") as temp:
                env = RestrictedBrowser(viewport_width=1440, viewport_height=900,
                    headless=True, browser_channel="chromium", start_page=url, single_tab_mode=True)
                await env.initialize()
                agent = Fara15Agent(Fara15AgentConfig(
                    client_config={"model": self.model_name, "base_url": self.endpoint, "api_key": "local"},
                    max_rounds=self.max_rounds, max_n_images=2, max_observation_chars=1000,
                    identity="fara_qwen35", critical_points="fara-1.5", save_screenshots=False,
                    auto_user_reply=False, image_budget_token_cap=5000,
                    extra_create_args={"max_tokens": 2048, "temperature": 0}))
                run_context = RunContext.create(environment=env,
                    task=Task(task_id=job["id"], instruction=task), output_dir=Path(temp))
                await agent.initialize(run_context)
                async with asyncio.timeout(600):
                    while True:
                        answer, _, _ = await agent.run(run_context)
                        job["answer"] = str(answer)[:8000]
                        if run_context.solver_log.status != SolverStatus.WAITING_FOR_USER:
                            job["status"] = "complete" if run_context.solver_log.status == SolverStatus.COMPLETE else "stopped"
                            break
                        job["status"] = "waiting_for_user"
                        text = await asyncio.wait_for(job["queue"].get(), timeout=300)
                        run_context.add_observation(UserMessage(content=text, message_type=UserMessageType.CRITICAL_POINT_RESPONSE))
                await agent.close(run_context)
                agent = None
        except asyncio.CancelledError:
            job["status"] = "cancelled"
            raise
        except TimeoutError:
            job.update(status="expired", answer="Browser task timed out.")
        except Exception as exc:
            job.update(status="error", answer=f"Fara harness failed: {type(exc).__name__}")
        finally:
            try:
                if agent is not None and run_context is not None:
                    await agent.close(run_context)
            finally:
                if env is not None:
                    await env.close()
