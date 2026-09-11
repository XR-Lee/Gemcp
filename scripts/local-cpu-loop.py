#!/usr/bin/env python3
"""Exercise the five local CPU flows against a running gemcp serve."""

from __future__ import annotations

import http.cookiejar
import json
import os
import shutil
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


STUDY_PREFIX = "cpu-loop"
REQUIRED_SCOPES = ("read", "submit", "configure", "operate_nodes")
LOCAL_PASSWORD = "local-process"


def fail(message: str) -> None:
    sys.stderr.write(message.rstrip() + "\n")
    raise SystemExit(1)


def decode_mcp_body(raw: str, content_type: str) -> dict:
    if "text/event-stream" in content_type:
        data_lines = []
        for line in raw.splitlines():
            if line.startswith("data:"):
                data_lines.append(line[5:].lstrip())
        raw = "\n".join(data_lines)
    return json.loads(raw) if raw.strip() else {}


class MCP:
    def __init__(self, origin: str, token: str) -> None:
        self.origin = origin.rstrip("/")
        self.token = token
        self.session_id = ""
        self._ident = 1

    def call(self, method: str, params=None, notification: bool = False) -> dict:
        payload = {"jsonrpc": "2.0", "method": method}
        if not notification:
            self._ident += 1
            payload["id"] = self._ident
        if params is not None:
            payload["params"] = params
        headers = {
            "Authorization": "Bearer " + self.token,
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if self.session_id:
            headers["Mcp-Session-Id"] = self.session_id
        req = urllib.request.Request(
            self.origin + "/mcp",
            data=json.dumps(payload).encode(),
            headers=headers,
            method="POST",
        )
        try:
            with urllib.request.urlopen(req, timeout=60) as resp:
                status = resp.status
                raw = resp.read().decode()
                content_type = resp.headers.get("Content-Type", "")
                self.session_id = resp.headers.get("Mcp-Session-Id", self.session_id)
        except urllib.error.HTTPError as exc:
            fail(f"FAIL POST /mcp {method}: HTTP {exc.code}\n{exc.read().decode()}")
        allowed = (202, 204) if notification else (200,)
        if status not in allowed:
            fail(f"FAIL POST /mcp {method}: HTTP {status}\n{raw}")
        if notification:
            return {}
        body = decode_mcp_body(raw, content_type)
        if body.get("error"):
            fail(f"FAIL POST /mcp {method}: {body['error']}")
        return body

    def initialize(self) -> None:
        info = self.call("initialize", {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "gemcp-local-cpu-loop", "version": "0"},
        })
        print("OK  MCP initialize", info.get("result", {}).get("serverInfo", {}), flush=True)
        self.call("notifications/initialized", {}, notification=True)

    def tool(self, name: str, arguments: dict | None = None) -> dict:
        body = self.call("tools/call", {"name": name, "arguments": arguments or {}})
        result = body.get("result") or {}
        if result.get("isError"):
            fail(f"FAIL MCP {name}: {result}")
        structured = result.get("structuredContent")
        if structured is None:
            fail(f"FAIL MCP {name}: missing structuredContent {result}")
        print(f"OK  MCP {name}", flush=True)
        return structured


class Owner:
    def __init__(self, origin: str, email: str, password: str) -> None:
        self.origin = origin.rstrip("/")
        self.email = email
        self.password = password
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
        self.csrf = ""

    def login(self) -> None:
        body = self._json("POST", "/api/v1/auth/login", {"email": self.email, "password": self.password})
        self.csrf = ((body.get("data") or {}).get("csrf_token") or "")
        if not self.csrf:
            fail("Owner login did not return csrf_token")
        print("OK  Owner login", flush=True)

    def _json(self, method: str, path: str, payload: dict | None = None) -> dict:
        headers = {"Accept": "application/json"}
        data = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(payload).encode()
        if method not in ("GET", "HEAD") and self.csrf:
            headers["X-CSRF-Token"] = self.csrf
        req = urllib.request.Request(self.origin + path, data=data, headers=headers, method=method)
        try:
            with self.opener.open(req, timeout=30) as resp:
                raw = resp.read().decode()
                return json.loads(raw) if raw.strip() else {}
        except urllib.error.HTTPError as exc:
            fail(f"FAIL {method} {path}: HTTP {exc.code}\n{exc.read().decode()}")

    def grant_scopes(self, project_id: str, token: str) -> None:
        listed = self._json("GET", f"/api/v1/projects/{project_id}/agent-tokens")
        tokens = ((listed.get("data") or {}).get("tokens") or [])
        match = None
        for item in tokens:
            prefix = item.get("prefix") or ""
            if prefix and token.startswith(prefix):
                match = item
                break
        if match is None:
            fail("could not match the Agent Token in Owner token list; re-run setup or local-http-smoke")
        scopes = list(match.get("scopes") or [])
        needed = [scope for scope in REQUIRED_SCOPES if scope not in scopes]
        if not needed:
            print("OK  Agent Token already has configure and operate_nodes", flush=True)
            return
        merged = []
        for scope in ("read", "submit", "cancel", "configure", "operate_nodes"):
            if scope in scopes or scope in needed:
                merged.append(scope)
        self._json("PATCH", f"/api/v1/projects/{project_id}/agent-tokens/{match['id']}", {"scopes": merged})
        print("OK  Owner granted Agent Token scopes", merged, flush=True)

    def confirm(self, project_id: str, proposal_id: str, digest: str) -> dict:
        body = self._json(
            "POST",
            f"/api/v1/projects/{project_id}/experiment-proposals/{proposal_id}/submit",
            {"confirmation_digest": digest, "confirmed": True},
        )
        print("OK  Owner confirmed prepared digest", flush=True)
        return body.get("data") or {}


def seed_fixture(repo_root: Path) -> Path:
    dest = Path(os.environ.get("GEMCP_MODELNET40_ROOT") or (Path.home() / "gemcp/datasets/modelnet40-mini"))
    src = repo_root / "examples/local-cpu/dataset"
    if not src.is_dir():
        fail(f"missing fixture {src}")
    dest.mkdir(parents=True, exist_ok=True)
    shutil.copytree(src, dest, dirs_exist_ok=True)
    shutil.copy2(repo_root / "examples/local-cpu/train.py", dest / "train.py")
    dest.joinpath("train.py").chmod(0o755)
    if not (dest / "meta.json").is_file() or not (dest / "train/chair/0001.off").is_file():
        fail(f"fixture seed failed under {dest}")
    print(f"OK  seeded ModelNet40-mini fixture at {dest}", flush=True)
    return dest


def node_by_kind(workspace: dict, kind: str) -> dict | None:
    study = workspace.get("study") or {}
    for node in study.get("nodes") or []:
        if node.get("kind") == kind:
            return node
    return None


def find_node(workspace: dict, kind: str, title: str) -> dict | None:
    study = workspace.get("study") or {}
    for node in study.get("nodes") or []:
        if node.get("kind") == kind and node.get("title") == title:
            return node
    return None


def print_graph(workspace: dict) -> None:
    study = workspace.get("study") or {}
    nodes = {node["id"]: node for node in study.get("nodes") or []}
    print("GRAPH nodes:", flush=True)
    for node in study.get("nodes") or []:
        extra = []
        if node.get("experiment_id"):
            extra.append(f"experiment={node['experiment_id']} state={node.get('experiment_state')}")
        if node.get("metric_name"):
            extra.append(f"{node['metric_name']}={node.get('metric_value')}")
        suffix = (" " + " ".join(extra)) if extra else ""
        print(f"  - {node['kind']}: {node['title']}{suffix}", flush=True)
    print("GRAPH edges:", flush=True)
    for edge in study.get("edges") or []:
        src = nodes.get(edge.get("from_id"), {})
        dst = nodes.get(edge.get("to_id"), {})
        print(
            f"  - {src.get('kind')} {src.get('title')!r} --{edge.get('relation')}--> {dst.get('kind')} {dst.get('title')!r}",
            flush=True,
        )


def decide(experiment: dict, workspace: dict) -> str:
    state = (experiment.get("state") or "").lower()
    metrics = experiment.get("metrics") or {}
    accuracy = metrics.get("overall_accuracy")
    study = workspace.get("study") or {}
    has_result = any(node.get("kind") == "result" for node in study.get("nodes") or [])
    has_observation = any(node.get("kind") == "observation" for node in study.get("nodes") or [])
    has_decision = any(node.get("kind") == "decision" for node in study.get("nodes") or [])
    if state != "succeeded" or not has_result:
        return "failure"
    if accuracy is None:
        return "new_observation"
    try:
        value = float(accuracy)
    except (TypeError, ValueError):
        return "new_observation"
    if value < 0.5:
        return "new_observation"
    if has_observation and has_decision:
        return "success"
    return "new_observation"


def main() -> int:
    repo_root = Path(sys.argv[1] if len(sys.argv) > 1 else os.getcwd())
    origin = os.environ.get("GEMCP_PUBLIC_URL", "http://127.0.0.1:8080").rstrip("/")
    token_file = Path(os.environ.get("GEMCP_DEV_AGENT_TOKEN_FILE") or (repo_root / ".gemcp-local-agent-token"))
    if not token_file.is_file():
        fail(f"missing Agent Token {token_file}; run ./scripts/local-http-smoke.sh first")
    token = token_file.read_text().strip()
    if not token:
        fail(f"empty Agent Token {token_file}")

    fixture = seed_fixture(repo_root)
    user = os.environ.get("USER") or os.environ.get("LOGNAME") or "ubuntu"

    mcp = MCP(origin, token)
    mcp.initialize()
    guide = mcp.tool("get_usage_guide")
    project_id = guide.get("project_id")
    if not project_id:
        fail("get_usage_guide did not return project_id")

    owner = Owner(
        origin,
        os.environ.get("GEMCP_DEV_OWNER_EMAIL", "owner@localhost"),
        os.environ.get("GEMCP_DEV_OWNER_PASSWORD", "local-dev-owner-password"),
    )
    owner.login()
    owner.grant_scopes(project_id, token)
    mcp = MCP(origin, token)
    mcp.initialize()

    options = mcp.tool("get_project_options")
    existing_local = None
    for node in options.get("ssh_cloud_nodes") or []:
        if node.get("host") in {"127.0.0.1", "localhost"}:
            existing_local = node
            break
    if existing_local:
        print(f"OK  reuse compute node {existing_local.get('label')} {existing_local.get('host')}", flush=True)
        compute = existing_local
    else:
        try:
            compute = mcp.tool("register_ssh_cloud_node", {
                "label": "local-cpu",
                "host": "127.0.0.1",
                "user": user,
                "auth_method": "password",
                "password": LOCAL_PASSWORD,
            })
        except SystemExit as exc:
            fail(
                "register_ssh_cloud_node failed. Enable GEMCP_SSH_CLOUD_ENABLED=true, "
                "GEMCP_LOCAL_PROCESS_ENABLED=true, and GEMCP_SCHEDULER_ENABLED=true in .env, "
                f"restart serve, and retry.\n{exc}"
            )

    environment = mcp.tool("register_environment", {
        "name": "local-cpu-host",
        "backend": "ssh_cloud",
        "image_uuid": "host",
        "set_default": True,
    })
    dataset = mcp.tool("register_dataset_binding", {
        "catalog": "modelnet40-mini",
        "backend": "ssh_cloud",
        "canonical_root": str(fixture),
    })
    print(
        f"FLOW1 compute={compute.get('id') or compute.get('label')} "
        f"environment={environment.get('name')} dataset={dataset.get('name')} "
        f"root={dataset.get('canonical_root')}",
        flush=True,
    )

    stamp = time.strftime("%Y%m%d-%H%M%S")
    study_name = f"{STUDY_PREFIX} {stamp}"
    question = "Can a CPU ModelNet40-mini fixture schedule, heartbeat, and close through Gemcp?"
    workspace = mcp.tool("update_research_workspace", {
        "study": {
            "name": study_name,
            "question": question,
            "summary": "Local CPU loop for issue 6. No NVIDIA.",
        }
    })
    question_node = node_by_kind(workspace, "question")
    if question_node is None:
        fail("Study did not create a question node")

    assumption_title = "New assumption: ModelNet40-mini is enough to schedule a CPU train"
    sub_title = "Sub-assumption: heartbeat scrape and close_run verify that assumption"
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "hypothesis",
            "title": assumption_title,
            "summary": "A two-class OFF fixture under an approved host path can be injected as GEMCP_DATASET_MODELNET40_MINI.",
            "from_node_id": question_node["id"],
            "relation": "leads_to",
        }
    })
    assumption = find_node(workspace, "hypothesis", assumption_title)
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "hypothesis",
            "title": sub_title,
            "summary": "get_experiment must show a scraped log_tail or last_heartbeat_at, then close_run writes the result.",
            "from_node_id": assumption["id"],
            "relation": "compares",
        }
    })
    plan_title = "Schedule train.py on the local host process"
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "plan",
            "title": plan_title,
            "summary": "prepare_experiment argv python3 train.py with cwd on the seeded fixture.",
            "from_node_id": assumption["id"],
            "relation": "leads_to",
        }
    })
    plan = find_node(workspace, "plan", plan_title)
    print_graph(workspace)
    print("FLOW3 schedule-task graph recorded (assumption vs sub-assumption verification)", flush=True)

    prepared = mcp.tool("prepare_experiment", {
        "argv": ["python3", str(fixture / "train.py")],
        "cwd": str(fixture),
        "runtime_preset": "smoke",
        "dataset": "modelnet40-mini",
        "from_node_id": plan["id"],
        "expected_metric": "overall_accuracy",
    })
    proposal = prepared.get("proposal")
    if not proposal:
        fail(f"prepare_experiment did not return a proposal: {prepared}")
    if not proposal.get("eligible"):
        fail(f"prepare_experiment not eligible: {proposal.get('checks')}")
    print(
        f"FLOW2 prepared proposal={proposal.get('id')} digest={proposal.get('confirmation_digest')} "
        f"backend={((proposal.get('resource') or {}).get('backend'))}",
        flush=True,
    )

    submitted = owner.confirm(project_id, proposal["id"], proposal["confirmation_digest"])
    experiment = (submitted.get("experiment") or submitted)
    experiment_id = experiment.get("id")
    if not experiment_id:
        fail(f"Owner confirm did not return an experiment: {submitted}")
    print(f"FLOW2 scheduled experiment={experiment_id} state={experiment.get('state')}", flush=True)

    heartbeat_seen = False
    deadline = time.time() + 180
    while time.time() < deadline:
        current = mcp.tool("get_experiment", {"experiment_id": experiment_id})
        log_tail = current.get("log_tail") or ""
        metrics = current.get("metrics") or {}
        heartbeat = current.get("last_heartbeat_at")
        state = current.get("state")
        if log_tail or metrics or heartbeat:
            heartbeat_seen = True
            print(
                f"FLOW4 heartbeat state={state} last_heartbeat_at={heartbeat} "
                f"metrics={metrics} log_chars={len(log_tail)}",
                flush=True,
            )
        if state in {"succeeded", "failed", "cancelled", "timed_out", "budget_stopped", "provider_error"}:
            experiment = current
            break
        time.sleep(2)
    else:
        fail("experiment did not reach a terminal state in 180s")

    if not heartbeat_seen:
        fail("Gemcp never ingested a heartbeat, log_tail, or metrics.json")
    if experiment.get("state") != "succeeded":
        fail(f"experiment ended {experiment.get('state')}: {experiment.get('failure_reason')}")

    workspace = mcp.tool("close_run", {
        "study_id": workspace["study"]["id"],
        "experiment_id": experiment_id,
        "title": "ModelNet40-mini CPU fixture finished",
        "summary": "Copied overall_accuracy from the scraped metrics.json. Do not infer from logs.",
        "status": "succeeded",
    })
    result_node = node_by_kind(workspace, "result")
    if result_node is None:
        fail("close_run did not write a result node")
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "observation",
            "title": "Heartbeat and metrics returned to the control plane",
            "summary": f"get_experiment showed last_heartbeat_at={experiment.get('last_heartbeat_at')} metrics={experiment.get('metrics')}",
            "from_node_id": assumption["id"],
            "relation": "leads_to",
            "occurred_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        }
    })
    observation = find_node(workspace, "observation", "Heartbeat and metrics returned to the control plane")
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "hypothesis",
            "id": assumption["id"],
            "title": assumption_title,
            "from_node_id": observation["id"],
            "relation": "supports",
        }
    })
    workspace = mcp.tool("update_research_workspace", {
        "node": {
            "study_id": workspace["study"]["id"],
            "kind": "decision",
            "title": "Keep the local CPU fixture as the no-NVIDIA path",
            "summary": "The five flows completed. A CLI can treat overall_accuracy >= 0.5 as success.",
            "from_node_id": result_node["id"],
            "relation": "leads_to",
        }
    })
    print_graph(workspace)
    decision = decide(experiment, workspace)
    evidence_url = f"{origin}/"
    print(
        f"FLOW5 summarized run on the graph experiment={experiment_id} "
        f"console={evidence_url} artifacts via list_artifacts and read_artifact",
        flush=True,
    )
    try:
        artifacts = mcp.tool("list_artifacts", {"experiment_id": experiment_id})
        print("OK  artifacts", artifacts, flush=True)
        names = artifacts.get("artifacts") or []
        if "metrics.json" in names:
            metrics = mcp.tool("read_artifact", {"experiment_id": experiment_id, "name": "metrics.json"})
            print("OK  read_artifact metrics.json", metrics, flush=True)
    except SystemExit:
        print("WARN list_artifacts skipped", flush=True)

    print(f"CLI_DECISION={decision}", flush=True)
    if decision == "failure":
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
