"""nabu-tts-export: checkpoint → one ONNX file + filled-in model card, with a parity check.

    uv run nabu-tts-export --checkpoint runs/smoke/checkpoints/latest.pt --out runs/smoke/model.onnx

ONNX interface (what the Go runtime in package tts expects):

  inputs   ipa_token_ids  int64   [1, T]  tokens from tts/symbols.txt (add_blank applied)
           speaker_id     int64   [1]
           noise_scale    float32 [1]
           length_scale   float32 [1]    > 1 is slower speech
           noise_scale_w  float32 [1]
  output   waveform       float32 [N]    mono, in [-1, 1] nominally, at nabu.sample_rate

  metadata nabu.format=1, nabu.symbols_sha256, nabu.sample_rate, nabu.add_blank,
           nabu.speakers (JSON list; index = speaker_id), nabu.languages (JSON),
           nabu.model_name, nabu.step, nabu.noise_scale, nabu.length_scale,
           nabu.noise_scale_w (defaults), nabu.exported_at

The parity check runs the same inputs through PyTorch and ONNX Runtime with all
noise scales at 0 (deterministic) and fails if the waveforms differ in length or
by more than --atol anywhere.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import string
import sys
import warnings
from pathlib import Path

import numpy as np
import torch

from nabu_tts.config import config_from_dict
from nabu_tts.symbols import load_symbols
from nabu_tts.train import build_generator, load_checkpoint

ONNX_FORMAT = "1"
INPUT_NAMES = ["ipa_token_ids", "speaker_id", "noise_scale", "length_scale", "noise_scale_w"]
OUTPUT_NAMES = ["waveform"]
DEFAULTS = {"noise_scale": 0.667, "length_scale": 1.0, "noise_scale_w": 0.8}  # upstream VITS
MODEL_CARD_TEMPLATE = Path(__file__).resolve().parents[2] / "MODEL_CARD.md"


class OnnxSynthesizer(torch.nn.Module):
    """Single-utterance inference with the exported signature."""

    def __init__(self, net_g: torch.nn.Module) -> None:
        super().__init__()
        self.net_g = net_g

    def forward(
        self,
        ipa_token_ids: torch.Tensor,
        speaker_id: torch.Tensor,
        noise_scale: torch.Tensor,
        length_scale: torch.Tensor,
        noise_scale_w: torch.Tensor,
    ) -> torch.Tensor:
        # Length from the data (every id is >= 0), so T stays dynamic in the graph.
        lengths = (ipa_token_ids >= 0).sum(dim=1)
        audio = self.net_g.infer(
            ipa_token_ids,
            lengths,
            sid=speaker_id,
            noise_scale=noise_scale,
            length_scale=length_scale,
            noise_scale_w=noise_scale_w,
        )[0]
        return audio[0, 0]


def parity_check(
    wrapper: OnnxSynthesizer,
    onnx_path: Path,
    n_vocab: int,
    n_speakers: int,
    add_blank: bool,
    atol: float,
    seed: int = 0,
) -> float:
    import onnxruntime as ort

    sess = ort.InferenceSession(str(onnx_path), providers=["CPUExecutionProvider"])
    g = torch.Generator().manual_seed(seed)
    worst = 0.0
    for n_tokens in (3, 17, 60):
        ids = torch.randint(2, n_vocab, (n_tokens,), generator=g)  # skip <pad>, <space>
        if add_blank:
            blanked = torch.zeros(2 * n_tokens + 1, dtype=torch.long)
            blanked[1::2] = ids
            ids = blanked
        ids = ids.unsqueeze(0)
        sid = torch.tensor([int(torch.randint(0, n_speakers, (1,), generator=g))])
        zero, one = torch.tensor([0.0]), torch.tensor([1.0])
        with torch.no_grad():
            ref = wrapper(ids, sid, zero, one, zero).numpy()
        got = sess.run(
            None,
            {
                "ipa_token_ids": ids.numpy(),
                "speaker_id": sid.numpy(),
                "noise_scale": zero.numpy(),
                "length_scale": one.numpy(),
                "noise_scale_w": zero.numpy(),
            },
        )[0]
        if ref.shape != got.shape:
            raise SystemExit(
                f"parity check FAILED ({n_tokens} tokens): "
                f"PyTorch length {ref.shape}, ONNX {got.shape}"
            )
        diff = float(np.max(np.abs(ref - got))) if ref.size else 0.0
        worst = max(worst, diff)
        print(f"  parity {n_tokens:3d} tokens: {ref.shape[0]} samples, max |diff| = {diff:.2e}")
        if diff > atol:
            raise SystemExit(f"parity check FAILED: max |diff| {diff:.2e} > atol {atol:.0e}")
    return worst


def render_model_card(
    ckpt: dict, onnx_path: Path, meta: dict[str, str], worst: float | None
) -> str:
    stats = ckpt.get("data_stats", {})
    langs = stats.get("languages", {})
    rows = (
        "\n".join(
            f"| {k} | {v['clips']:,} | {v['hours']:.2f} | {v['speakers']} |"
            for k, v in langs.items()
        )
        or "| (none recorded) | | | |"
    )
    cfg = ckpt["config"]
    values = {
        "model_name": meta["nabu.model_name"],
        "onnx_file": onnx_path.name,
        "step": str(ckpt["step"]),
        "exported_at": meta["nabu.exported_at"],
        "symbols_sha256": ckpt["symbols_sha256"],
        "sample_rate": str(cfg["data"]["sample_rate"]),
        "num_speakers": str(len(ckpt["speakers"])),
        "language_rows": rows,
        "manifests": "\n".join(f"- `{m}`" for m in stats.get("manifests", [])) or "- (none)",
        "parity": "not run" if worst is None else f"passed, max |diff| {worst:.2e}",
        "synthetic_warning": (
            "\n> [!WARNING]\n> This model was trained on SYNTHETIC smoke-test data (tones, not "
            "speech). It exists only to test the pipeline. Do not publish it.\n"
            if "synthetic" in langs
            else ""
        ),
    }
    template = string.Template(MODEL_CARD_TEMPLATE.read_text(encoding="utf-8"))
    return template.safe_substitute(values)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-tts-export", description=__doc__.split("\n")[0])
    ap.add_argument("--checkpoint", required=True)
    ap.add_argument("--out", required=True, help="output .onnx path")
    ap.add_argument("--symbols", help="symbol table (default: tts/symbols.txt)")
    ap.add_argument("--opset", type=int, default=17)
    ap.add_argument(
        "--atol", type=float, default=1e-3, help="parity tolerance on samples in [-1, 1]"
    )
    ap.add_argument("--no-check", action="store_true", help="skip the ONNX Runtime parity check")
    args = ap.parse_args(argv)

    ckpt = load_checkpoint(args.checkpoint)
    symbols = load_symbols(args.symbols)
    if ckpt["symbols_sha256"] != symbols.sha256():
        print(
            "checkpoint was trained with a different tts/symbols.txt "
            f"({ckpt['symbols_sha256'][:12]} ≠ {symbols.sha256()[:12]}); refusing to export.",
            file=sys.stderr,
        )
        return 2
    cfg = config_from_dict(ckpt["config"], "checkpoint config")
    net_g = build_generator(cfg, len(symbols), len(ckpt["speakers"]))
    net_g.load_state_dict(ckpt["net_g"])
    net_g.eval()
    net_g.dec.remove_weight_norm()
    wrapper = OnnxSynthesizer(net_g).eval()

    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    example_ids = torch.tensor([symbols.intersperse([2, 3, 4, 5, 6])])
    example = (
        example_ids,
        torch.tensor([0]),
        torch.tensor([DEFAULTS["noise_scale"]]),
        torch.tensor([DEFAULTS["length_scale"]]),
        torch.tensor([DEFAULTS["noise_scale_w"]]),
    )
    with warnings.catch_warnings():
        warnings.simplefilter("ignore")
        # The TorchScript-based exporter handles VITS's data-dependent output
        # length (durations → frames) with dynamic axes.
        torch.onnx.export(
            wrapper,
            example,
            str(out),
            dynamo=False,
            input_names=INPUT_NAMES,
            output_names=OUTPUT_NAMES,
            dynamic_axes={"ipa_token_ids": {1: "tokens"}, "waveform": {0: "samples"}},
            opset_version=args.opset,
        )

    import onnx

    model = onnx.load(str(out))
    langs = sorted(ckpt.get("data_stats", {}).get("languages", {}))
    meta = {
        "nabu.format": ONNX_FORMAT,
        "nabu.model_name": f"{cfg.run_name}@{ckpt['step']}",
        "nabu.step": str(ckpt["step"]),
        "nabu.symbols_sha256": symbols.sha256(),
        "nabu.sample_rate": str(cfg.data.sample_rate),
        "nabu.add_blank": "1" if cfg.data.add_blank else "0",
        "nabu.speakers": json.dumps(ckpt["speakers"]),
        "nabu.languages": json.dumps(langs),
        "nabu.noise_scale": str(DEFAULTS["noise_scale"]),
        "nabu.length_scale": str(DEFAULTS["length_scale"]),
        "nabu.noise_scale_w": str(DEFAULTS["noise_scale_w"]),
        "nabu.exported_at": dt.datetime.now(dt.UTC).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }
    del model.metadata_props[:]
    for k, v in meta.items():
        model.metadata_props.add(key=k, value=v)
    model.producer_name = "nabu-tts-export"
    onnx.checker.check_model(model)
    onnx.save(model, str(out))
    print(f"exported {out} ({out.stat().st_size / 1e6:.1f} MB)")

    worst = None
    if not args.no_check:
        worst = parity_check(
            wrapper, out, len(symbols), len(ckpt["speakers"]), cfg.data.add_blank, args.atol
        )
        print(f"parity check passed (atol {args.atol:.0e})")

    card = out.with_suffix(".model-card.md")
    card.write_text(render_model_card(ckpt, out, meta, worst), encoding="utf-8")
    print(f"model card draft: {card} (fill in the TODO sections before publishing)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
