"""nabu-tts-smoke-data: a tiny SYNTHETIC dataset that proves the pipeline runs.

The "speech" is tones: each IPA symbol gets a fixed pitch and each speaker a
pitch offset, so a model can learn an alignment in a few hundred CPU steps.
It is not speech and teaches nothing about pronunciation. Token strings are
random sequences of the symbols a rule file can emit (default: egy-allen), so
the smoke model can synthesize those rule outputs end to end.

    uv run nabu-tts-smoke-data --out runs/smoke-data
"""

from __future__ import annotations

import argparse
import math
import random
from pathlib import Path

import numpy as np

from nabu_tts.audio import write_wav
from nabu_tts.coverage import DEFAULT_RULES
from nabu_tts.manifest import Entry, write_manifest
from nabu_tts.rules import read_rule_outputs
from nabu_tts.symbols import load_symbols


def render(
    tokens: list[str],
    pitch: dict[str, float],
    offset: float,
    sr: int,
    rng: random.Random,
) -> np.ndarray:
    parts = []
    for tok in tokens:
        if tok == " ":
            parts.append(np.zeros(int(sr * 0.06), dtype=np.float32))
            continue
        dur = rng.uniform(0.08, 0.16)
        t = np.arange(int(sr * dur)) / sr
        f = pitch[tok] * offset
        tone = 0.25 * np.sin(2 * math.pi * f * t) + 0.08 * np.sin(4 * math.pi * f * t)
        env = np.minimum(1.0, np.minimum(t, dur - t) / 0.01)
        parts.append((tone * env).astype(np.float32))
    silence = np.zeros(int(sr * 0.05), dtype=np.float32)
    return np.concatenate([silence, *parts, silence])


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-tts-smoke-data", description=__doc__.split("\n")[0])
    ap.add_argument("--out", default="runs/smoke-data")
    ap.add_argument("--rules", default=str(DEFAULT_RULES))
    ap.add_argument("--symbols")
    ap.add_argument("--clips", type=int, default=96)
    ap.add_argument("--speakers", type=int, default=2)
    ap.add_argument("--sample-rate", type=int, default=16000)
    ap.add_argument("--seed", type=int, default=0)
    args = ap.parse_args(argv)

    symbols = load_symbols(args.symbols)
    inventory = read_rule_outputs(args.rules).code_points(symbols)
    if not inventory:
        ap.error(f"{args.rules} has no IPA outputs")
    rng = random.Random(args.seed)
    # Spread pitches over 200-800 Hz in symbol-table order (arbitrary, deterministic).
    ordered = sorted(inventory, key=symbols.id)
    pitch = {s: 200.0 * 4 ** (i / max(1, len(ordered) - 1)) for i, s in enumerate(ordered)}

    out = Path(args.out)
    (out / "wavs").mkdir(parents=True, exist_ok=True)
    entries = []
    for i in range(args.clips):
        spk = i % args.speakers
        words = [
            "".join(rng.choice(inventory) for _ in range(rng.randint(2, 5)))
            for _ in range(rng.randint(2, 4))
        ]
        ipa = symbols.normalize(" ".join(words))
        audio = render(list(ipa), pitch, 1.0 + 0.15 * spk, args.sample_rate, rng)
        rel = f"wavs/{i:04d}.wav"
        write_wav(out / rel, audio, args.sample_rate)
        entries.append(
            Entry(
                audio_path=rel,
                ipa=ipa,
                language="synthetic",
                speaker_id=f"synthetic-{spk}",
                duration=round(len(audio) / args.sample_rate, 3),
                text="",
                source="nabu-tts-smoke-data (synthetic tones, not speech)",
            )
        )
    n = write_manifest(out / "manifest.jsonl", entries)
    print(f"wrote {n} synthetic clips to {out / 'manifest.jsonl'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
