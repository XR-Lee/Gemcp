package runner

import (
	"fmt"
	"net/url"
	"strings"
)

const bootstrapScript = `import ctypes
import json
import os
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request

BASE_URL = os.environ.pop("GEMCP_RUNNER_URL", "").rstrip("/")
TOKEN = os.environ.pop("GEMCP_RUNNER_TOKEN", "")
try:
    ctypes.CDLL(None).prctl(4, 0, 0, 0, 0)
except Exception:
    pass

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

OPENER = urllib.request.build_opener(NoRedirect())

def request(path, payload=None, retries=1):
    body = None
    headers = {"Authorization": "Bearer " + TOKEN, "Accept": "application/json"}
    if payload is not None:
        body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        headers["Content-Type"] = "application/json"
    last_error = None
    for attempt in range(retries):
        req = urllib.request.Request(BASE_URL + path, data=body, headers=headers, method="POST" if body is not None else "GET")
        try:
            return OPENER.open(req, timeout=30)
        except urllib.error.HTTPError as error:
            last_error = error
            if error.code < 500 or attempt + 1 == retries:
                raise
        except urllib.error.URLError as error:
            last_error = error
            if attempt + 1 == retries:
                raise
        time.sleep(min(2 ** attempt, 8))
    raise last_error

def get_json(path):
    with request(path, retries=3) as response:
        return json.load(response)["data"]

def post_event(payload, retries=5):
    with request("/api/v1/runner/events", payload, retries=retries) as response:
        return json.load(response)["data"]

def safe_extract(archive_path, destination):
    root = os.path.realpath(destination)
    with tarfile.open(archive_path, "r:gz") as archive:
        for member in archive.getmembers():
            target = os.path.realpath(os.path.join(root, member.name))
            if target != root and not target.startswith(root + os.sep):
                raise RuntimeError("source archive contains an unsafe path")
            if member.isdev() or member.isfifo():
                raise RuntimeError("source archive contains a special file")
            if member.issym():
                link = os.path.realpath(os.path.join(os.path.dirname(target), member.linkname))
                if link != root and not link.startswith(root + os.sep):
                    raise RuntimeError("source archive contains an unsafe symlink")
            if member.islnk():
                link = os.path.realpath(os.path.join(root, member.linkname))
                if link != root and not link.startswith(root + os.sep):
                    raise RuntimeError("source archive contains an unsafe hard link")
        archive.extractall(root)

def download_source(path, maximum):
    total = 0
    with request("/api/v1/runner/source", retries=3) as response, open(path, "wb") as output:
        length = response.headers.get("Content-Length")
        if length and int(length) > maximum:
            raise RuntimeError("source archive exceeds the declared size limit")
        while True:
            chunk = response.read(1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > maximum:
                raise RuntimeError("source archive exceeds the declared size limit")
            output.write(chunk)

def terminate(process, grace):
    if process.poll() is not None:
        return
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    end = time.monotonic() + grace
    while process.poll() is None and time.monotonic() < end:
        time.sleep(0.25)
    if process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass

def normalized_stop_reason(value):
    if value == "emergency":
        return "emergency"
    if value in ("timeout", "provision_timeout"):
        return "timeout"
    return "cancelled"

def log_tail(path, maximum=65536):
    try:
        with open(path, "rb") as source:
            source.seek(0, os.SEEK_END)
            size = source.tell()
            source.seek(max(0, size - maximum))
            value = source.read(maximum).decode("utf-8", errors="replace").encode("utf-8")
            return value[-maximum:].decode("utf-8", errors="ignore")
    except FileNotFoundError:
        return ""

def metrics(output_path):
    path = os.path.join(output_path, "metrics.json")
    try:
        if os.path.getsize(path) > 65536:
            return {}
        with open(path, "r", encoding="utf-8") as source:
            value = json.load(source)
            if not isinstance(value, dict):
                return {}
            encoded = json.dumps(value, separators=(",", ":"), allow_nan=False).encode("utf-8")
            return value if len(encoded) <= 60000 else {}
    except (FileNotFoundError, OSError, ValueError, RecursionError, OverflowError, TypeError):
        return {}

def write_result(output_path, result):
    temporary = os.path.join(output_path, ".gemcp-result.json.tmp")
    final = os.path.join(output_path, "gemcp-result.json")
    with open(temporary, "w", encoding="utf-8") as output:
        json.dump(result, output, separators=(",", ":"), sort_keys=True)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, final)

def main():
    spec = get_json("/api/v1/runner/spec")
    output_path = spec["output_path"]
    os.makedirs(output_path, mode=0o700, exist_ok=True)
    work_path = os.path.join(tempfile.gettempdir(), "gemcp-" + spec["attempt_id"])
    shutil.rmtree(work_path, ignore_errors=True)
    os.makedirs(work_path, mode=0o700)
    archive_path = os.path.join(work_path, "source.tar.gz")
    download_source(archive_path, int(spec["source_max_bytes"]))
    source_path = os.path.join(work_path, "source")
    os.makedirs(source_path, mode=0o700)
    safe_extract(archive_path, source_path)
    os.remove(archive_path)

    control = post_event({"type": "started"})
    log_path = os.path.join(output_path, "run.log")
    reason = "completed"
    exit_code = 0
    process = None
    started_wall = time.time()
    started_mono = time.monotonic()
    try:
        if control.get("stop_requested"):
            reason = normalized_stop_reason(control.get("stop_reason"))
            exit_code = 143
        else:
            environment = dict(os.environ)
            environment.pop("GEMCP_RUNNER_URL", None)
            environment.pop("GEMCP_RUNNER_TOKEN", None)
            environment["GEMCP_OUTPUT_DIR"] = output_path
            with open(log_path, "ab", buffering=0) as log:
                process = subprocess.Popen(
                    ["/bin/sh", "-lc", spec["command"]], cwd=source_path,
                    stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                    env=environment, start_new_session=True,
                )
                interval = max(5, int(spec["heartbeat_interval_seconds"]))
                local_limit = int(spec["max_runtime_seconds"]) + int(spec["timeout_extension_seconds"])
                while process.poll() is None:
                    time.sleep(interval)
                    if time.monotonic() - started_mono >= local_limit:
                        reason = "timeout"
                        terminate(process, int(spec["termination_grace_seconds"]))
                        break
                    try:
                        control = post_event({"type": "heartbeat"}, retries=3)
                    except Exception:
                        control = {}
                    if control.get("stop_requested"):
                        reason = normalized_stop_reason(control.get("stop_reason"))
                        terminate(process, int(spec["termination_grace_seconds"]))
                        break
                result = process.wait()
                exit_code = result if result >= 0 else min(255, 128 - result)
                if reason in ("timeout", "cancelled", "emergency") and exit_code == 0:
                    exit_code = 143
    except Exception as error:
        reason = "runner_error"
        exit_code = 70
        with open(log_path, "ab") as log:
            message = (type(error).__name__ + ": " + str(error))[:2048] + "\n"
            log.write(message.encode("utf-8", errors="replace"))
        if process is not None:
            terminate(process, int(spec["termination_grace_seconds"]))

    finished_wall = time.time()
    result = {
        "attempt_id": spec["attempt_id"], "experiment_id": spec["experiment_id"],
        "exit_code": exit_code, "reason": reason,
        "started_at_unix": started_wall, "finished_at_unix": finished_wall,
    }
    write_result(output_path, result)
    try:
        post_event({
            "type": "finished", "exit_code": exit_code, "reason": reason,
            "log_tail": log_tail(log_path), "metrics": metrics(output_path),
        }, retries=5)
    except Exception as error:
        with open(log_path, "ab") as log:
            log.write(("Gemcp completion callback failed: " + type(error).__name__ + "\n").encode("utf-8"))
    return exit_code

try:
    sys.exit(main())
except Exception as error:
    sys.stderr.write("Gemcp Runner bootstrap failed: " + type(error).__name__ + ": " + str(error)[:512] + "\n")
    sys.exit(70)
`

const downloader = `import os,urllib.request;u=os.environ["GEMCP_RUNNER_URL"].rstrip("/");t=os.environ["GEMCP_RUNNER_TOKEN"];q=urllib.request.Request(u+"/api/v1/runner/bootstrap",headers={"Authorization":"Bearer "+t});N=type("NoRedirect",(urllib.request.HTTPRedirectHandler,),{"redirect_request":lambda *args:None});o=urllib.request.build_opener(N());c=o.open(q,timeout=30).read(131073);assert len(c)<=131072;exec(compile(c,"gemcp-runner","exec"))`

func BootstrapScript() string { return bootstrapScript }

func LaunchCommand(publicURL, token string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(publicURL), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return "", fmt.Errorf("Runner public URL must be a credential-free HTTPS origin")
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 4096 {
		return "", fmt.Errorf("Runner token is invalid")
	}
	script := "set -eu; PYTHON=$(command -v python3 || command -v python); test -n \"$PYTHON\"; " +
		"GEMCP_RUNNER_URL=" + shellQuote(parsed.String()) + " GEMCP_RUNNER_TOKEN=" + shellQuote(token) +
		" \"$PYTHON\" -c " + shellQuote(downloader)
	return "/bin/sh -lc " + shellQuote(script), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
