package diagnostic

import (
	"strconv"
	"strings"
)

const expectedGPUCountPlaceholder = "__GEMCP_EXPECTED_GPU_COUNT__"

const gpuConnectivityCommand = `set -eu
umask 077
out=${GEMCP_OUTPUT_DIR:?}
mkdir -p "$out"
report="$out/diagnostic-report.txt"
: > "$report"
printf 'suite=gpu_connectivity\n' >> "$report"
printf 'working_directory=%s\n' "$PWD" >> "$report"
printf 'output_directory=%s\n' "$out" >> "$report"
if ! command -v nvidia-smi >/dev/null 2>&1; then
  printf 'result=failed\nerror=nvidia-smi-not-found\n' >> "$report"
  printf '{"diagnostic_passed":false,"suite":"gpu_connectivity","error_type":"nvidia_smi_not_found"}\n' > "$out/metrics.json"
  cat "$report"
  exit 70
fi
if ! listing=$(nvidia-smi -L 2>&1); then
  printf 'result=failed\nerror=nvidia-smi-list-failed\n%s\n' "$listing" >> "$report"
  printf '{"diagnostic_passed":false,"suite":"gpu_connectivity","error_type":"nvidia_smi_failed"}\n' > "$out/metrics.json"
  cat "$report"
  exit 70
fi
printf '%s\n' "$listing" >> "$report"
if ! details=$(nvidia-smi --query-gpu=index,name,uuid,driver_version,memory.total --format=csv,noheader 2>&1); then
  printf 'result=failed\nerror=nvidia-smi-query-failed\n%s\n' "$details" >> "$report"
  printf '{"diagnostic_passed":false,"suite":"gpu_connectivity","error_type":"nvidia_smi_query_failed"}\n' > "$out/metrics.json"
  cat "$report"
  exit 70
fi
printf '%s\n' "$details" >> "$report"
gpu_count=$(nvidia-smi --query-gpu=index --format=csv,noheader 2>/dev/null | wc -l | tr -d ' ')
case "$gpu_count" in ''|*[!0-9]*) gpu_count=0 ;; esac
expected_gpu_count=__GEMCP_EXPECTED_GPU_COUNT__
if [ "$gpu_count" -ne "$expected_gpu_count" ]; then
  printf 'result=failed\nerror=unexpected-visible-gpu-count\nexpected_gpu_count=%s\nactual_gpu_count=%s\n' "$expected_gpu_count" "$gpu_count" >> "$report"
  printf '{"diagnostic_passed":false,"suite":"gpu_connectivity","gpu_count":%s,"expected_gpu_count":%s,"error_type":"unexpected_gpu_count"}\n' "$gpu_count" "$expected_gpu_count" > "$out/metrics.json"
  cat "$report"
  exit 70
fi
printf 'result=passed\n' >> "$report"
printf '{"diagnostic_passed":true,"suite":"gpu_connectivity","gpu_count":%s,"expected_gpu_count":%s,"source_readable":true,"output_writable":true}\n' "$gpu_count" "$expected_gpu_count" > "$out/metrics.json"
cat "$report"`

const pytorchCUDACommand = `set -eu
umask 077
out=${GEMCP_OUTPUT_DIR:?}
mkdir -p "$out"
python_bin=
for candidate in python3 /root/miniconda3/bin/python3; do
  if command -v "$candidate" >/dev/null 2>&1; then
    python_bin="$candidate"
    break
  fi
done
if [ -z "$python_bin" ]; then
  printf 'suite=pytorch_cuda\nresult=failed\nerror=python-not-found\n' > "$out/diagnostic-report.txt"
  printf '{"diagnostic_passed":false,"suite":"pytorch_cuda","error_type":"python_not_found"}\n' > "$out/metrics.json"
  cat "$out/diagnostic-report.txt"
  exit 70
fi
"$python_bin" - <<'PY'
import json
import math
import os
import platform
import sys
import time

out = os.environ["GEMCP_OUTPUT_DIR"]
metrics = {"diagnostic_passed": False, "suite": "pytorch_cuda"}
report = ["suite=pytorch_cuda", "python=" + sys.version.replace("\n", " "), "platform=" + platform.platform()]
try:
    import torch
    report.append("torch=" + str(torch.__version__))
    if not torch.cuda.is_available():
        raise RuntimeError("torch.cuda.is_available returned false")
    count = int(torch.cuda.device_count())
    expected_count = __GEMCP_EXPECTED_GPU_COUNT__
    if count != expected_count:
        raise RuntimeError("PyTorch reported an unexpected CUDA device count: expected %d, got %d" % (expected_count, count))
    device = torch.device("cuda:0")
    name = torch.cuda.get_device_name(device)
    report.append("cuda_devices=" + str(count))
    report.append("cuda_name=" + name)
    started = time.monotonic()
    left = torch.randn((512, 512), device=device)
    right = torch.randn((512, 512), device=device)
    result = left @ right
    value = float(result[0, 0].item())
    torch.cuda.synchronize(device)
    elapsed_ms = int((time.monotonic() - started) * 1000)
    if not math.isfinite(value):
        raise RuntimeError("CUDA matrix multiplication returned a non-finite value")
    metrics.update({
        "diagnostic_passed": True,
        "gpu_count": count,
        "expected_gpu_count": expected_count,
        "cuda_compute": True,
        "cuda_elapsed_ms": elapsed_ms,
        "torch_version": str(torch.__version__)[:80],
        "gpu_name": name[:160],
    })
    report.append("cuda_elapsed_ms=" + str(elapsed_ms))
    report.append("result=passed")
except BaseException as error:
    error_message = str(error).replace("\n", " ")[:400]
    lowered = error_message.lower()
    semantic_error = type(error).__name__[:100]
    if "unexpected cuda device count" in lowered:
        semantic_error = "unexpected_gpu_count"
    elif "cuda" in lowered:
        semantic_error = "cuda_runtime_error"
    metrics["error_type"] = semantic_error
    report.append("result=failed")
    report.append("error_type=" + semantic_error)
    report.append("error=" + error_message)
finally:
    metrics_path = os.path.join(out, "metrics.json")
    temporary = metrics_path + ".tmp"
    with open(temporary, "w", encoding="utf-8") as output:
        json.dump(metrics, output, separators=(",", ":"), sort_keys=True, allow_nan=False)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, metrics_path)
    with open(os.path.join(out, "diagnostic-report.txt"), "w", encoding="utf-8") as output:
        output.write("\n".join(report) + "\n")
    print("\n".join(report))
if not metrics["diagnostic_passed"]:
    raise SystemExit(70)
PY`

func commandForSuite(suite string, gpuCount int) (string, int, error) {
	if gpuCount < 1 {
		return "", 0, invalid("resource profile must request at least one GPU")
	}
	withExpectedGPUCount := func(command string) string {
		return strings.ReplaceAll(command, expectedGPUCountPlaceholder, strconv.Itoa(gpuCount))
	}
	switch suite {
	case SuiteGPUConnectivity:
		return withExpectedGPUCount(gpuConnectivityCommand), 180, nil
	case SuitePyTorchCUDA:
		return withExpectedGPUCount(pytorchCUDACommand), 300, nil
	default:
		return "", 0, invalid("suite must be gpu_connectivity or pytorch_cuda")
	}
}
