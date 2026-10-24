#!/usr/bin/env python3
"""Simulated experiments for the Gemcp research benchmark. Standard library only.

Each task is a small research question with a known answer, so a scorer can
check whether an Agent explored efficiently and recorded the truth. The
formulas use only + - * / and table lookups so internal/researchbench
reproduces them bit for bit.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

# Seed-to-seed variation. augment-noise has one lucky seed (1) that looks like
# a gain; the mean effect is zero.
LR_JITTER = {1: 0.0, 2: 0.0006, 3: -0.0004, 4: 0.0003, 5: -0.0002}
AUGMENT_OFFSET = {1: 0.016, 2: -0.006, 3: 0.001, 4: -0.011, 5: 0.0}
DEPTH_JITTER = {1: 0.0, 2: 0.002, 3: -0.001, 4: 0.001, 5: -0.002}
BATCH_JITTER = {1: 0.0, 2: 0.001, 3: -0.001, 4: 0.0005, 5: -0.0005}

# The true effect of augmentation on accuracy.
AUGMENT_EFFECT = 0.0

OUT_OF_MEMORY = 137


def lr_sweep(args: argparse.Namespace) -> dict:
    # A narrow sweet spot at log_lr = -27 (learning rate ~ 0.002).
    d = (args.log_lr + 27) / 2
    return {"val_accuracy": 0.80 + 0.12 / (1 + d * d) + LR_JITTER[args.seed]}


def augment_noise(args: argparse.Namespace) -> dict:
    # Only the seed moves the number; one lucky seed looks like a gain.
    return {"val_accuracy": 0.85 + AUGMENT_EFFECT * args.augment + AUGMENT_OFFSET[args.seed]}


def depth_false(args: argparse.Namespace) -> dict:
    # Deeper models overfit: validation loss rises past depth 2.
    if args.depth < 2:
        return {"val_loss": 0.43 + DEPTH_JITTER[args.seed]}
    return {"val_loss": 0.40 + 0.03 * (args.depth - 2) + DEPTH_JITTER[args.seed]}


def batch_crash(args: argparse.Namespace) -> dict | None:
    # Batches of 2^8 and above do not fit in memory.
    if args.batch_log2 >= 8:
        return None
    return {"val_accuracy": 0.86 + 0.01 * (args.batch_log2 - 5) + BATCH_JITTER[args.seed]}


TASKS = {
    "lr-sweep": lr_sweep,
    "augment-noise": augment_noise,
    "depth-false": depth_false,
    "batch-crash": batch_crash,
}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--task", required=True, choices=sorted(TASKS))
    parser.add_argument("--seed", type=int, default=1, choices=sorted(LR_JITTER))
    parser.add_argument("--log-lr", dest="log_lr", type=int, default=-40)
    parser.add_argument("--augment", type=int, default=0, choices=[0, 1])
    parser.add_argument("--depth", type=int, default=2)
    parser.add_argument("--batch-log2", dest="batch_log2", type=int, default=5)
    args = parser.parse_args()

    metrics = TASKS[args.task](args)
    print(f"{args.task} starting seed={args.seed}", flush=True)
    if metrics is None:
        print("RuntimeError: CUDA out of memory (simulated)", file=sys.stderr, flush=True)
        return OUT_OF_MEMORY
    output_dir = Path(os.environ.get("GEMCP_OUTPUT_DIR") or "outputs")
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "metrics.json").write_text(json.dumps(metrics) + "\n")
    print(f"{args.task} wrote {json.dumps(metrics)}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
