"""Serve the base model plus a LoRA adapter with an OpenAI-compatible API.

    nabu-llm-serve --config translate/train/configs/full.yaml [--port 8090]

Then: make run-api LLM_URL=http://127.0.0.1:8090/v1 LLM_MODEL=<model>.
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

from nabu_llm.train import check_storage, load_config


def command(cfg: dict, host: str, port: int) -> list[str]:
    cmd = [
        sys.executable,
        "-m",
        "mlx_lm",
        "server",
        "--model",
        cfg["model"],
        "--host",
        host,
        "--port",
        str(port),
    ]
    if (Path(cfg["adapters"]) / "adapters.safetensors").is_file():
        cmd += ["--adapter-path", cfg["adapters"]]
    return cmd


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-llm-serve", description=__doc__.split("\n")[0])
    ap.add_argument("--config", required=True)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=8090)
    args = ap.parse_args(argv)
    cfg = load_config(args.config)
    check_storage()
    cmd = command(cfg, args.host, args.port)
    if "--adapter-path" not in cmd:
        print(f"warning: no adapters in {cfg['adapters']}; serving the base model", file=sys.stderr)
    print(" ".join(cmd), flush=True)
    os.execv(cmd[0], cmd)
    return 0  # unreachable


if __name__ == "__main__":
    raise SystemExit(main())
