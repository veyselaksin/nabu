"""nabu-tts-prepare: one Common Voice locale → filtered WAVs + JSONL manifest.

    uv run nabu-tts-prepare --cv-dir ~/cv/cv-corpus-XX/kab --out runs/data/kab \\
        --top-speakers 20 --max-clips-per-speaker 2000 --workers 8

Pipeline (each rejection is counted in <out>/report.json, nothing is dropped silently):
 1. votes:    up_votes > down_votes and up_votes >= --min-up-votes
 2. text:     orthography → IPA with Epitran (see donors.EpitranConverter)
 3. speakers: optionally keep only the --top-speakers with the most clips
              remaining after steps 1-2, and cap clips per speaker
 4. audio:    decode, trim leading/trailing silence, reject too quiet / clipped /
              noisy audio, keep --min-duration ≤ duration ≤ --max-duration,
              resample to --sample-rate, write 16-bit mono WAV
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
from collections import Counter, defaultdict
from concurrent.futures import ProcessPoolExecutor
from dataclasses import asdict, dataclass
from pathlib import Path

from nabu_tts.audio import (
    QualityThresholds,
    decode_file,
    measure,
    reject_reason,
    resample,
    trim_silence,
    write_wav,
)
from nabu_tts.commonvoice import Clip, read_validated, speaker_label, votes_ok
from nabu_tts.donors import EpitranConverter, load_donors
from nabu_tts.manifest import Entry, write_manifest
from nabu_tts.symbols import load_symbols


@dataclass(frozen=True)
class AudioJob:
    src: str
    dst: str
    sample_rate: int
    min_duration: float
    max_duration: float
    thresholds: QualityThresholds


def process_audio(job: AudioJob) -> tuple[str | None, float]:
    """Returns (rejection reason or None, duration in seconds)."""
    try:
        x, sr = decode_file(job.src)
    except Exception:
        return "decode_error", 0.0
    q = measure(x, sr, job.thresholds)
    reason = reject_reason(q, job.thresholds)
    if reason:
        return reason, 0.0
    x = trim_silence(x, sr, job.thresholds)
    dur = len(x) / sr
    if dur < job.min_duration:
        return "too_short", dur
    if dur > job.max_duration:
        return "too_long", dur
    x = resample(x, sr, job.sample_rate)
    # Peak-normalize down only if resampling overshot (avoid introducing clipping).
    peak = float(abs(x).max()) if len(x) else 0.0
    if peak > 0.99:
        x = x * (0.99 / peak)
    write_wav(job.dst, x, job.sample_rate)
    return None, len(x) / job.sample_rate


def _stable_key(s: str) -> str:
    return hashlib.sha256(s.encode()).hexdigest()


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="nabu-tts-prepare", description=__doc__.split("\n")[0])
    ap.add_argument("--cv-dir", required=True, help="extracted Common Voice locale directory")
    ap.add_argument("--out", required=True, help="output directory (wavs/, manifest.jsonl)")
    ap.add_argument("--locale", help="Common Voice locale (default: name of --cv-dir)")
    ap.add_argument("--epitran", help="Epitran code (default: from configs/donors.yaml)")
    ap.add_argument("--donors", help="donor candidates YAML (default: configs/donors.yaml)")
    ap.add_argument("--symbols", help="symbol table (default: tts/symbols.txt)")
    ap.add_argument("--sample-rate", type=int, default=22050)
    ap.add_argument("--min-duration", type=float, default=1.0)
    ap.add_argument("--max-duration", type=float, default=12.0)
    ap.add_argument("--min-up-votes", type=int, default=2)
    ap.add_argument("--top-speakers", type=int, default=0, help="keep only N speakers (0 = all)")
    ap.add_argument("--max-clips-per-speaker", type=int, default=0, help="0 = no cap")
    ap.add_argument("--min-snr-db", type=float, default=QualityThresholds.min_snr_db)
    ap.add_argument(
        "--max-clipped-fraction", type=float, default=QualityThresholds.max_clipped_fraction
    )
    ap.add_argument("--limit", type=int, default=0, help="stop after N rows (for quick tests)")
    ap.add_argument("--workers", type=int, default=os.cpu_count() or 1)
    ap.add_argument(
        "--allow-abjad",
        action="store_true",
        help="allow a donor marked abjad: true (short vowels unrecoverable; see README)",
    )
    args = ap.parse_args(argv)

    cv_dir = Path(args.cv_dir)
    locale = args.locale or cv_dir.name
    donors = load_donors(args.donors)
    donor = donors.get(locale)
    code = args.epitran or (donor.epitran_code if donor else None)
    if not code:
        ap.error(f"no Epitran code for locale {locale!r}: pass --epitran or add it to donors.yaml")
    if donor and donor.abjad and not args.allow_abjad:
        ap.error(
            f"{locale} is marked abjad in donors.yaml: Epitran output would lack short vowels. "
            "This needs a project decision (see 'The unvocalized-script problem' in "
            "tts/train/README.md); pass --allow-abjad to proceed anyway."
        )

    symbols = load_symbols(args.symbols)
    conv = EpitranConverter(code, symbols)
    out = Path(args.out)
    (out / "wavs").mkdir(parents=True, exist_ok=True)
    counts: Counter[str] = Counter()
    unknown_symbols: Counter[str] = Counter()
    unmapped_chars: Counter[str] = Counter()

    # 1-2: votes and text.
    kept: list[tuple[Clip, str]] = []
    cache: dict[str, object] = {}
    for i, clip in enumerate(read_validated(cv_dir)):
        if args.limit and i >= args.limit:
            break
        counts["rows"] += 1
        if not votes_ok(clip, args.min_up_votes):
            counts["rejected_votes"] += 1
            continue
        c = cache.get(clip.sentence)
        if c is None:
            c = cache[clip.sentence] = conv.convert(clip.sentence)
        if c.ipa is None:
            counts[f"rejected_text_{c.reason}"] += 1
            if c.reason == "unknown_ipa_symbols":
                unknown_symbols.update(c.detail.split())
            elif c.reason == "unmapped_chars":
                unmapped_chars.update(c.detail.split())
            continue
        kept.append((clip, c.ipa))

    # 3: speakers.
    per_speaker: dict[str, list[tuple[Clip, str]]] = defaultdict(list)
    for clip, ipa in kept:
        per_speaker[clip.client_id].append((clip, ipa))
    speakers = sorted(per_speaker, key=lambda s: (-len(per_speaker[s]), _stable_key(s)))
    if args.top_speakers:
        dropped = speakers[args.top_speakers :]
        counts["rejected_not_top_speaker"] += sum(len(per_speaker[s]) for s in dropped)
        speakers = speakers[: args.top_speakers]
    selected: list[tuple[Clip, str]] = []
    for s in speakers:
        clips = sorted(per_speaker[s], key=lambda ci: _stable_key(ci[0].path))
        if args.max_clips_per_speaker and len(clips) > args.max_clips_per_speaker:
            counts["rejected_speaker_cap"] += len(clips) - args.max_clips_per_speaker
            clips = clips[: args.max_clips_per_speaker]
        selected.extend(clips)

    # 4: audio.
    thresholds = QualityThresholds(
        min_snr_db=args.min_snr_db, max_clipped_fraction=args.max_clipped_fraction
    )
    jobs = [
        AudioJob(
            src=str(cv_dir / "clips" / clip.path),
            dst=str(out / "wavs" / (Path(clip.path).stem + ".wav")),
            sample_rate=args.sample_rate,
            min_duration=args.min_duration,
            max_duration=args.max_duration,
            thresholds=thresholds,
        )
        for clip, _ in selected
    ]
    entries: list[Entry] = []
    with ProcessPoolExecutor(max_workers=max(1, args.workers)) as pool:
        results = pool.map(process_audio, jobs, chunksize=16)
        for n, ((clip, ipa), job, (reason, dur)) in enumerate(
            zip(selected, jobs, results, strict=True), 1
        ):
            if reason:
                counts[f"rejected_audio_{reason}"] += 1
            else:
                entries.append(
                    Entry(
                        audio_path=os.path.relpath(job.dst, out),
                        ipa=ipa,
                        language=locale,
                        speaker_id=speaker_label(locale, clip.client_id),
                        duration=round(dur, 3),
                        text=clip.sentence,
                        source=f"common_voice/{locale}/clips/{clip.path}",
                    )
                )
            if n % 1000 == 0:
                print(f"  audio {n}/{len(jobs)}", file=sys.stderr)

    entries.sort(key=lambda e: e.audio_path)
    counts["kept"] = write_manifest(out / "manifest.jsonl", entries)
    report = {
        "locale": locale,
        "epitran_code": code,
        "symbols_sha256": symbols.sha256(),
        "options": {k: v for k, v in vars(args).items() if k not in ("cv_dir", "out")},
        "thresholds": asdict(thresholds),
        "counts": dict(sorted(counts.items())),
        "hours": round(sum(e.duration for e in entries) / 3600, 3),
        "speakers": len({e.speaker_id for e in entries}),
        "unknown_ipa_symbols": {f"U+{ord(k):04X} {k}": v for k, v in unknown_symbols.most_common()},
        "unmapped_chars": dict(unmapped_chars.most_common(50)),
    }
    (out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(report["counts"], indent=2))
    print(f"{counts['kept']} clips, {report['hours']} h, {report['speakers']} speakers → {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
