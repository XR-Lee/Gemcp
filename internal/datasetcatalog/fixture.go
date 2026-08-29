package datasetcatalog

import (
	"os"
	"path/filepath"
	"strings"
)

const modelnet40MiniHomeSuffix = "gemcp/datasets/modelnet40-mini"

const modelnet40MiniMeta = `{
  "name": "modelnet40-mini",
  "classes": ["chair", "table"],
  "n_classes": 2,
  "split": "tiny-cpu-fixture",
  "notes": "Not the full ModelNet40 corpus. Enough samples to exercise Gemcp schedule, heartbeat, and close_run on CPU."
}
`

const modelnet40MiniOFF = `OFF
8 6 0
-0.5 -0.5 -0.5
0.5 -0.5 -0.5
0.5 0.5 -0.5
-0.5 0.5 -0.5
-0.5 -0.5 0.5
0.5 -0.5 0.5
0.5 0.5 0.5
-0.5 0.5 0.5
4 0 1 2 3
4 4 5 6 7
4 0 1 5 4
4 2 3 7 6
4 0 3 7 4
4 1 2 6 5
`

const modelnet40MiniTrain = `#!/usr/bin/env python3
"""CPU-only ModelNet40-mini fixture trainer. No NVIDIA, no extra packages."""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path


def count_off(root: Path, split: str) -> int:
    folder = root / split
    if not folder.is_dir():
        return 0
    return sum(1 for path in folder.rglob("*.off") if path.is_file())


def main() -> int:
    root = Path(os.environ.get("GEMCP_DATASET_MODELNET40_MINI", "")).expanduser()
    if not root.is_dir():
        print("missing GEMCP_DATASET_MODELNET40_MINI", file=sys.stderr)
        return 2
    meta_path = root / "meta.json"
    if not meta_path.is_file():
        print("missing meta.json under", root, file=sys.stderr)
        return 2
    meta = json.loads(meta_path.read_text())
    n_train = count_off(root, "train")
    n_test = count_off(root, "test")
    print("modelnet40-mini starting", flush=True)
    print(f"classes={meta.get('n_classes')} train={n_train} test={n_test}", flush=True)
    time.sleep(8)
    accuracy = 0.75 if n_train >= 2 and n_test >= 1 else 0.0
    output_dir = Path(os.environ.get("GEMCP_OUTPUT_DIR") or "outputs")
    output_dir.mkdir(parents=True, exist_ok=True)
    metrics = {
        "overall_accuracy": accuracy,
        "n_classes": int(meta.get("n_classes") or 0),
        "n_train": n_train,
        "n_test": n_test,
    }
    (output_dir / "metrics.json").write_text(json.dumps(metrics, indent=2) + "\n")
    print("modelnet40-mini wrote", output_dir / "metrics.json", flush=True)
    print("modelnet40-mini done", flush=True)
    return 0 if accuracy > 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
`

func DefaultModelNet40MiniRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "/gemcp/datasets/modelnet40-mini"
	}
	return filepath.Join(home, modelnet40MiniHomeSuffix)
}

func SeedModelNet40Mini(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultModelNet40MiniRoot()
	}
	files := map[string]string{
		"meta.json":            modelnet40MiniMeta,
		"train/chair/0001.off": modelnet40MiniOFF,
		"train/table/0001.off": modelnet40MiniOFF,
		"test/chair/0001.off":  modelnet40MiniOFF,
		"train.py":             modelnet40MiniTrain,
	}
	for relative, body := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return os.Chmod(filepath.Join(root, "train.py"), 0o755)
}
