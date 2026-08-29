#!/usr/bin/env python3
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
    # Deterministic fixture "accuracy": one class seen in train and test.
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
