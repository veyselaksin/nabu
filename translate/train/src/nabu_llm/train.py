"""Fine-tune the translation model with LoRA (mlx-lm, Apple Silicon).

    nabu-llm-train --config translate/train/configs/smoke.yaml

The training data comes from ``nabu-translate export-sft`` (``make llm-data``):
chat-format ``train.jsonl``/``valid.jsonl`` whose prompts are built by the same
Go code as at inference, retrieval included. Only the assistant turn (the
transliteration) is trained on (``mask_prompt``).

Adapters go to ``adapters`` (under NABU_CHECKPOINT_DIR), the mlx-lm config and
log to ``runs`` (under NABU_RUNS_DIR). Both are derived from CC BY-SA 4.0 data:
keep them local (DATA_LICENSES.md).
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

import yaml

from nabu_tts.paths import expand

REQUIRED = ("run", "model", "data", "adapters", "runs", "iters")


def load_config(path: str | os.PathLike[str]) -> dict[str, Any]:
    """Read a config and expand ${NABU_*} roots in its paths."""
    with open(path, encoding="utf-8") as f:
        cfg = yaml.safe_load(f) or {}
    missing = [k for k in REQUIRED if k not in cfg]
    if missing:
        raise ValueError(f"{path}: missing {', '.join(missing)}")
    for key in ("data", "adapters", "runs"):
        cfg[key] = expand(str(cfg[key]))
    return cfg


def mlx_config(cfg: dict[str, Any]) -> dict[str, Any]:
    """The mlx_lm.lora config for a Nabu config."""
    lora = cfg.get("lora", {})
    return {
        "model": cfg["model"],
        "train": True,
        "data": cfg["data"],
        "fine_tune_type": "lora",
        "mask_prompt": True,
        "adapter_path": cfg["adapters"],
        "iters": int(cfg["iters"]),
        "batch_size": int(cfg.get("batch_size", 1)),
        "num_layers": int(cfg.get("num_layers", 16)),
        "learning_rate": float(cfg.get("learning_rate", 1e-5)),
        "max_seq_length": int(cfg.get("max_seq_length", 2048)),
        "steps_per_report": int(cfg.get("steps_per_report", 10)),
        "steps_per_eval": int(cfg.get("steps_per_eval", 200)),
        "val_batches": int(cfg.get("val_batches", 25)),
        "save_every": int(cfg.get("save_every", 100)),
        "grad_checkpoint": bool(cfg.get("grad_checkpoint", True)),
        "seed": int(cfg.get("seed", 0)),
        "lora_parameters": {
            "rank": int(lora.get("rank", 8)),
            "scale": float(lora.get("scale", 20.0)),
            "dropout": float(lora.get("dropout", 0.0)),
        },
    }


def check_data(data: str) -> dict[str, Any]:
    """Fail early when export-sft has not been run; return its manifest."""
    d = Path(data)
    for name in ("train.jsonl", "valid.jsonl"):
        if not (d / name).is_file() or (d / name).stat().st_size == 0:
            raise FileNotFoundError(f"{d / name} missing or empty; run: make llm-data")
    manifest = d / "manifest.json"
    return json.loads(manifest.read_text()) if manifest.is_file() else {}


def check_storage() -> None:
    """Model downloads go to HF_HOME; refuse to fill the internal disk."""
    if os.environ.get("MODELS_ROOT") and not os.environ.get("HF_HOME"):
        raise RuntimeError("HF_HOME is not set: source models-env.zsh or use make llm-train")


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-llm-train", description=__doc__.split("\n")[0])
    ap.add_argument("--config", required=True)
    ap.add_argument("--dry-run", action="store_true", help="write the mlx-lm config and stop")
    args = ap.parse_args(argv)

    cfg = load_config(args.config)
    check_storage()
    data_manifest = check_data(cfg["data"])
    runs = Path(cfg["runs"])
    runs.mkdir(parents=True, exist_ok=True)
    Path(cfg["adapters"]).mkdir(parents=True, exist_ok=True)

    mcfg = mlx_config(cfg)
    mlx_path = runs / "mlx-lora.yaml"
    mlx_path.write_text(yaml.safe_dump(mcfg, sort_keys=False))
    run_info = {
        "started": dt.datetime.now(dt.UTC).isoformat(timespec="seconds"),
        "config": cfg,
        "model_revision": cfg.get("model_revision"),
        "data_manifest": data_manifest,
        "license": "adapters derived from CC-BY-SA-4.0 data; base model Apache-2.0; keep local",
    }
    (runs / "run.json").write_text(json.dumps(run_info, indent=2, ensure_ascii=False))
    print(f"mlx-lm config: {mlx_path}\nadapters: {cfg['adapters']}", flush=True)
    if args.dry_run:
        return 0

    cmd = [sys.executable, "-m", "mlx_lm", "lora", "--config", str(mlx_path)]
    log_path = runs / "train.log"
    with open(log_path, "a", encoding="utf-8") as log:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        assert proc.stdout is not None
        for line in proc.stdout:
            sys.stdout.write(line)
            log.write(line)
            log.flush()
        code = proc.wait()
    print(f"log: {log_path}", flush=True)
    return code


if __name__ == "__main__":
    raise SystemExit(main())
