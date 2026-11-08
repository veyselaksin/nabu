"""TLA (Thesaurus Linguae Aegyptiae): planned Middle Egyptian source."""

from __future__ import annotations

from nabu_data import Source, describe

# TODO(source): verify license, share-alike and attribution terms on the TLA site,
# and which export/dataset is the canonical download.
SOURCE = Source(
    key="tla",
    name="TLA — Thesaurus Linguae Aegyptiae",
    url="https://thesaurus-linguae-aegyptiae.de/",
    license="UNVERIFIED — check before downloading",
    share_alike=None,
    notes="Identify the canonical bulk export and its license before writing the downloader.",
)


def main() -> int:
    print(describe(SOURCE))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
