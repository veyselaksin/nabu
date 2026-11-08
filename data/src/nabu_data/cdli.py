"""CDLI (Cuneiform Digital Library Initiative): planned Sumerian source."""

from __future__ import annotations

from nabu_data import Source, describe

# TODO(source): verify license and share-alike terms on the CDLI site.
# Catalogue metadata, transliterations and images may be licensed differently.
SOURCE = Source(
    key="cdli",
    name="CDLI — Cuneiform Digital Library Initiative",
    url="https://cdli.mpiwg-berlin.mpg.de/",
    license="UNVERIFIED — check before downloading",
    share_alike=None,
    notes="Licensing may differ between catalogue data, transliterations and images.",
)


def main() -> int:
    print(describe(SOURCE))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
