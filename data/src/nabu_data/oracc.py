"""ORACC (Open Richly Annotated Cuneiform Corpus): planned Sumerian source."""

from __future__ import annotations

from nabu_data import Source, describe

# TODO(source): verify license and share-alike terms on the ORACC site.
# Licensing may differ per ORACC sub-project, so record it per project.
SOURCE = Source(
    key="oracc",
    name="ORACC — Open Richly Annotated Cuneiform Corpus",
    url="https://oracc.museum.upenn.edu/",
    license="UNVERIFIED — check before downloading",
    share_alike=None,
    notes="Terms may vary per sub-project; record the license of each project used.",
)


def main() -> int:
    print(describe(SOURCE))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
