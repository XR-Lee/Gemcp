package runner

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	runnerUserAgent               = "Gemcp-Runner/1"
	maxProviderLaunchCommandBytes = 4096
)

const bootstrapScript = `import ctypes
import http.client
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
LAUNCH_LOG = os.environ.get("GEMCP_LAUNCH_LOG", "")
USER_AGENT = "` + runnerUserAgent + `"
FAILURE_STAGE = "bootstrap_failed_before_spec"

def launch_log(message):
    if not LAUNCH_LOG:
        return
    try:
        os.makedirs(os.path.dirname(LAUNCH_LOG), exist_ok=True)
        with open(LAUNCH_LOG, "a", encoding="utf-8") as output:
            output.write(message + "\n")
    except Exception:
        pass

launch_log("gemcp-launch-runner-entered")
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
    headers = {"Authorization": "Bearer " + TOKEN, "Accept": "application/json", "User-Agent": USER_AGENT}
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

def report_stage(stage, error_type="", retries=1):
    marker = "gemcp-launch-stage-" + stage
    if error_type:
        marker += "-" + error_type
    launch_log(marker)
    payload = {"type": "diagnostic", "stage": stage}
    if error_type:
        payload["error_type"] = error_type
    try:
        post_event(payload, retries=retries)
    except Exception as error:
        launch_log("gemcp-launch-stage-report-failed-" + stage + "-" + type(error).__name__)

def post_started(seconds_remaining, runtime_info):
    payload = {"type": "started", "runtime_info": runtime_info}
    if not seconds_remaining:
        return post_event(payload, retries=5)
    retry_until = time.monotonic() + max(0, float(seconds_remaining) - 30)
    delay = 1
    while True:
        try:
            return post_event(payload, retries=1)
        except urllib.error.HTTPError:
            raise
        except (urllib.error.URLError, http.client.HTTPException, TimeoutError, ConnectionError, OSError, ValueError) as error:
            last_error = error
        launch_log("gemcp-launch-started-callback-retry-" + type(last_error).__name__)
        remaining = retry_until - time.monotonic()
        if remaining <= 0:
            raise last_error
        time.sleep(min(delay, remaining))
        delay = min(delay * 2, 15)

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
    temporary = path + ".part"
    last_error = None
    transient_errors = (urllib.error.URLError, http.client.IncompleteRead, http.client.RemoteDisconnected, TimeoutError, ConnectionError, OSError)
    for attempt in range(3):
        try:
            total = 0
            declared = None
            with request("/api/v1/runner/source", retries=1) as response, open(temporary, "wb") as output:
                length = response.headers.get("Content-Length")
                if length:
                    declared = int(length)
                    if declared > maximum:
                        raise RuntimeError("source archive exceeds the declared size limit")
                while True:
                    chunk = response.read(1024 * 1024)
                    if not chunk:
                        break
                    total += len(chunk)
                    if total > maximum:
                        raise RuntimeError("source archive exceeds the declared size limit")
                    output.write(chunk)
                if declared is not None and total != declared:
                    raise http.client.IncompleteRead(b"", declared - total)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, path)
            return
        except urllib.error.HTTPError:
            try:
                os.remove(temporary)
            except FileNotFoundError:
                pass
            raise
        except transient_errors as error:
            last_error = error
            try:
                os.remove(temporary)
            except FileNotFoundError:
                pass
            launch_log("gemcp-launch-source-download-retry-" + type(error).__name__)
            if attempt + 1 == 3:
                raise
            time.sleep(2 ** attempt)
    raise last_error if last_error is not None else RuntimeError("source download failed")

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

def execution_argv(spec):
    mode = spec.get("execution_mode", "shell")
    if mode == "shell":
        command = spec.get("command")
        if not isinstance(command, str) or not command or "\x00" in command:
            raise RuntimeError("invalid shell execution specification")
        return ["/bin/sh", "-lc", command]
    if mode != "argv":
        raise RuntimeError("unsupported execution mode")
    if spec.get("command"):
        raise RuntimeError("argv execution must not include a command string")
    argv = spec.get("argv")
    if not isinstance(argv, list) or not argv or len(argv) > 256:
        raise RuntimeError("invalid argv execution specification")
    total = 0
    for index, value in enumerate(argv):
        if not isinstance(value, str) or (index == 0 and not value) or "\x00" in value:
            raise RuntimeError("invalid argv execution specification")
        total += len(value.encode("utf-8"))
        if total > 65536:
            raise RuntimeError("argv execution specification is too large")
    return argv

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

def observed_runtime(source_path, output_path):
    devices = []
    try:
        output = subprocess.check_output(
            ["nvidia-smi", "--query-gpu=index,uuid,name", "--format=csv,noheader,nounits"],
            stderr=subprocess.DEVNULL, timeout=5, text=True,
        )
        for line in output.splitlines()[:16]:
            parts = [part.strip() for part in line.split(",", 2)]
            if len(parts) != 3:
                continue
            devices.append({"index": int(parts[0]), "uuid": parts[1], "name": parts[2][:120]})
    except (FileNotFoundError, OSError, ValueError, subprocess.SubprocessError):
        devices = []
    visible = os.environ.get("CUDA_VISIBLE_DEVICES", "")
    if len(visible) > 255 or any(character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789,._:-" for character in visible):
        visible = ""
    return {
        "source": "runner_observed", "working_directory": source_path, "output_directory": output_path,
        "cuda_visible_devices": visible, "gpu_devices": devices,
    }

def write_result(output_path, result):
    temporary = os.path.join(output_path, ".gemcp-result.json.tmp")
    final = os.path.join(output_path, "gemcp-result.json")
    with open(temporary, "w", encoding="utf-8") as output:
        json.dump(result, output, separators=(",", ":"), sort_keys=True)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, final)

def main():
    global FAILURE_STAGE
    report_stage("runner_entered")
    spec = get_json("/api/v1/runner/spec")
    report_stage("spec_loaded")
    FAILURE_STAGE = "bootstrap_failed_during_source_download"
    output_path = spec["output_path"]
    os.makedirs(output_path, mode=0o700, exist_ok=True)
    work_path = os.path.join(tempfile.gettempdir(), "gemcp-" + spec["attempt_id"])
    shutil.rmtree(work_path, ignore_errors=True)
    os.makedirs(work_path, mode=0o700)
    archive_path = os.path.join(work_path, "source.tar.gz")
    download_source(archive_path, int(spec["source_max_bytes"]))
    FAILURE_STAGE = "bootstrap_failed_during_source_extract"
    report_stage("source_downloaded")
    source_path = os.path.join(work_path, "source")
    os.makedirs(source_path, mode=0o700)
    safe_extract(archive_path, source_path)
    os.remove(archive_path)
    report_stage("source_extracted")

    FAILURE_STAGE = "bootstrap_failed_during_started_callback"
    control = post_started(int(spec.get("provisioning_seconds_remaining", 0)), observed_runtime(source_path, output_path))
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
            environment.pop("GEMCP_LAUNCH_LOG", None)
            environment["GEMCP_OUTPUT_DIR"] = output_path
            process_argv = execution_argv(spec)
            with open(log_path, "ab", buffering=0) as log:
                process = subprocess.Popen(
                    process_argv, cwd=source_path,
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
                        control = post_event({
                            "type": "heartbeat", "log_tail": log_tail(log_path, 60000), "metrics": metrics(output_path),
                        }, retries=3)
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
    launch_log("gemcp-launch-runner-failed-" + type(error).__name__)
    report_stage(FAILURE_STAGE, type(error).__name__, retries=3)
    sys.stderr.write("Gemcp Runner bootstrap failed: " + type(error).__name__ + ": " + str(error)[:512] + "\n")
    sys.exit(70)
`

const downloader = `import os
import time
import urllib.error
import urllib.request

launch_log_path = os.environ.get("GEMCP_LAUNCH_LOG", "")

def launch_log(message):
    if not launch_log_path:
        return
    try:
        os.makedirs(os.path.dirname(launch_log_path), exist_ok=True)
        with open(launch_log_path, "a", encoding="utf-8") as output:
            output.write(message + "\n")
    except Exception:
        pass

origin = os.environ["GEMCP_RUNNER_URL"].rstrip("/")
token = os.environ["GEMCP_RUNNER_TOKEN"]
request = urllib.request.Request(origin + "/api/v1/runner/bootstrap", headers={
    "Authorization": "Bearer " + token,
    "Accept": "application/json",
    "User-Agent": "` + runnerUserAgent + `",
})
NoRedirect = type("NoRedirect", (urllib.request.HTTPRedirectHandler,), {
    "redirect_request": lambda *args: None,
})
opener = urllib.request.build_opener(NoRedirect())
content = None
last_error = None
launch_log("gemcp-launch-bootstrap-download-started")
for delay in (0, 1, 2, 4):
    if delay:
        time.sleep(delay)
    try:
        with opener.open(request, timeout=30) as response:
            content = response.read(131073)
        break
    except urllib.error.HTTPError as error:
        launch_log("gemcp-launch-bootstrap-http-error-" + str(error.code))
        raise
    except Exception as error:
        last_error = error
if content is None:
    launch_log("gemcp-launch-bootstrap-download-failed-" + type(last_error).__name__)
    raise last_error if last_error is not None else RuntimeError("bootstrap download failed")
if len(content) > 131072:
    launch_log("gemcp-launch-bootstrap-download-too-large")
    raise RuntimeError("bootstrap download exceeded 128 KiB")
launch_log("gemcp-launch-bootstrap-download-complete")
exec(compile(content, "gemcp-runner", "exec"))`

func BootstrapScript() string { return bootstrapScript }

func LaunchCommand(publicURL, token string) (string, error) {
	return launchCommand(publicURL, token, "")
}

func LaunchCommandForOutput(publicURL, token, outputPath string) (string, error) {
	logPath, err := launchLogPath(outputPath)
	if err != nil {
		return "", err
	}
	return launchCommand(publicURL, token, logPath)
}

func launchCommand(publicURL, token, logPath string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(publicURL), "/"))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Trim(parsed.Path, "/") != "" {
		return "", fmt.Errorf("Runner public URL must be a credential-free HTTPS origin")
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 4096 {
		return "", fmt.Errorf("Runner token is invalid")
	}
	program := `import os;os.environ["GEMCP_RUNNER_URL"]=` + strconv.Quote(parsed.String()) +
		`;os.environ["GEMCP_RUNNER_TOKEN"]=` + strconv.Quote(token)
	if logPath != "" {
		program += `;os.environ["GEMCP_LAUNCH_LOG"]=` + strconv.Quote(logPath)
	}
	program += ";" + downloader
	payload := base64.StdEncoding.EncodeToString([]byte(program))
	python := "/root/miniconda3/bin/python3"
	base64Command := "/usr/bin/base64"
	command := "set -eu; "
	for _, delay := range []int{1, 2, 4, 8, 16, 32, 64, 128, 45} {
		command += fmt.Sprintf("test -x %s && test -x %s || sleep %d; ", python, base64Command, delay)
	}
	command += "test -x " + python + "; test -x " + base64Command + "; "
	command += "printf %s " + payload + " | " + base64Command + " -d | " + python
	if len(command) > maxProviderLaunchCommandBytes {
		return "", fmt.Errorf("Runner launch command exceeds the Provider limit")
	}
	return command, nil
}

func launchLogPath(outputPath string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(outputPath))
	parts := strings.Split(strings.TrimPrefix(cleaned, "/"), "/")
	if len(parts) != 6 || parts[0] != "root" || parts[1] != "autodl-fs" || parts[2] != "projects" || parts[4] != "experiments" {
		return "", fmt.Errorf("Runner output path is invalid")
	}
	if _, err := uuid.Parse(parts[3]); err != nil {
		return "", fmt.Errorf("Runner output path has an invalid Project ID")
	}
	if _, err := uuid.Parse(parts[5]); err != nil {
		return "", fmt.Errorf("Runner output path has an invalid Experiment ID")
	}
	return cleaned + "/gemcp-launch.log", nil
}
