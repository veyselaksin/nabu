"""Reading a Common Voice release (CC0) for one locale.

Expected layout (as extracted from a Common Voice download):

    <cv_dir>/validated.tsv
    <cv_dir>/clips/<path from the TSV>   (mp3)

Nabu does not download Common Voice: get it from Mozilla (the download page
asks you to accept its terms, including not trying to identify speakers).
"""

from __future__ import annotations

import csv
import hashlib
import sys
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Clip:
    client_id: str
    path: str
    sentence: str
    up_votes: int
    down_votes: int
    locale: str


def read_validated(cv_dir: str | Path) -> Iterator[Clip]:
    tsv = Path(cv_dir) / "validated.tsv"
    csv.field_size_limit(sys.maxsize)
    with open(tsv, encoding="utf-8", newline="") as f:
        # Sentences may contain quote characters; Common Voice TSVs are unquoted.
        reader = csv.DictReader(f, delimiter="\t", quoting=csv.QUOTE_NONE)
        missing = {"client_id", "path", "sentence", "up_votes", "down_votes"} - set(
            reader.fieldnames or []
        )
        if missing:
            raise ValueError(f"{tsv}: missing columns {sorted(missing)}")
        for row in reader:
            yield Clip(
                client_id=row["client_id"],
                path=row["path"],
                sentence=row["sentence"] or "",
                up_votes=int(row["up_votes"] or 0),
                down_votes=int(row["down_votes"] or 0),
                locale=row.get("locale") or "",
            )


def votes_ok(clip: Clip, min_up_votes: int) -> bool:
    """Positive validation: more up- than down-votes and at least min_up_votes."""
    return clip.up_votes > clip.down_votes and clip.up_votes >= min_up_votes


def speaker_label(locale: str, client_id: str) -> str:
    """Stable pseudonymous speaker label; does not contain the Common Voice client_id."""
    return f"{locale}-{hashlib.sha256(client_id.encode()).hexdigest()[:10]}"
