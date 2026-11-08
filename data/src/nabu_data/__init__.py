"""Corpus download and cleaning scripts (stubs).

Nothing here downloads anything yet. Each module describes one source and
prints its URL and license. Downloaded corpora must go under
``data/corpora/`` (git-ignored) and must never be committed.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

__all__ = ["Source", "corpora_dir", "describe"]


@dataclass(frozen=True, slots=True)
class Source:
    """A corpus Nabu may download. Keep in sync with DATA_LICENSES.md."""

    key: str
    name: str
    url: str
    license: str
    share_alike: bool | None  # None = not yet verified
    notes: str


def corpora_dir() -> Path:
    """Where downloads will be written: $NABU_DATA_DIR or ./data/corpora."""
    return Path(os.environ.get("NABU_DATA_DIR", "data/corpora"))


def describe(source: Source) -> str:
    sa = {True: "yes", False: "no", None: "unverified"}[source.share_alike]
    return (
        f"{source.name}\n"
        f"  url:         {source.url}\n"
        f"  license:     {source.license}\n"
        f"  share-alike: {sa}\n"
        f"  notes:       {source.notes}\n"
        f"  target dir:  {corpora_dir() / source.key}\n"
        f"Download is not implemented yet; nothing was fetched."
    )
