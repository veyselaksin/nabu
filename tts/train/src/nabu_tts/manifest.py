"""Training manifests: JSON Lines, one clip per line.

Required fields (all writers and readers agree on these):

    audio_path  str    16-bit mono PCM WAV, relative to the manifest's directory
    ipa         str    IPA, already normalized by SymbolTable.normalize
    language    str    Common Voice locale (or "synthetic" for smoke data)
    speaker_id  str    pseudonymous speaker label, e.g. "kab-3fa9c1d2e0"
    duration    float  seconds

Writers may add fields (e.g. text, source); readers ignore unknown fields.
"""

from __future__ import annotations

import json
from collections.abc import Iterable, Iterator
from dataclasses import asdict, dataclass
from pathlib import Path

REQUIRED = ("audio_path", "ipa", "language", "speaker_id", "duration")


@dataclass(frozen=True)
class Entry:
    audio_path: str
    ipa: str
    language: str
    speaker_id: str
    duration: float
    text: str = ""
    source: str = ""


def write_manifest(path: str | Path, entries: Iterable[Entry]) -> int:
    n = 0
    with open(path, "w", encoding="utf-8") as f:
        for e in entries:
            f.write(json.dumps(asdict(e), ensure_ascii=False) + "\n")
            n += 1
    return n


def read_manifest(path: str | Path) -> Iterator[tuple[Entry, Path]]:
    """Yield (entry, absolute audio path)."""
    path = Path(path)
    base = path.resolve().parent
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, 1):
            if not line.strip():
                continue
            raw = json.loads(line)
            missing = [k for k in REQUIRED if k not in raw]
            if missing:
                raise ValueError(f"{path}:{lineno}: missing fields {missing}")
            e = Entry(
                audio_path=raw["audio_path"],
                ipa=raw["ipa"],
                language=raw["language"],
                speaker_id=raw["speaker_id"],
                duration=float(raw["duration"]),
                text=raw.get("text", ""),
                source=raw.get("source", ""),
            )
            yield e, (base / e.audio_path)
