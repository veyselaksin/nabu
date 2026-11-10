# data/: download and build tools

Go module with one command, `nabu-data`. It downloads Nabu's upstream data
and builds derived files from it. **No corpus data is committed.** Downloads
go to `data/corpora/<source>/`, or `$NABU_DATA_DIR/<source>`, which is
git-ignored and excluded from Docker images.

```sh
go run ./data/cmd/nabu-data sources               # every source, its license and pinned files
go run ./data/cmd/nabu-data fetch unikemet tla    # download (make data-fetch)
go run ./data/cmd/nabu-data build-signs           # → script/signs/egy-unikemet-uniliterals.tsv (make signs)
go run ./data/cmd/nabu-data build-lexicon         # → data/corpora/tla/lexicon-egy.tsv (make lexicon)
```

| Source | What Nabu uses | License | Output |
| --- | --- | --- | --- |
| `unikemet`: Unicode Unikemet database 18.0.0 | sign values (`kEH_FVal`) and functions (`kEH_Func`) | Unicode-3.0 | sign list, **committed** (permissive) |
| `tla`: TLA Earlier Egyptian sentences, corpus v18 | aligned hieroglyphs + transliteration | CC BY-SA 4.0 | spelling lexicon, **local only** |
| `oracc`, `cdli` | – | unverified | `fetch` refuses until the license is recorded |

Rules:

- **Pinned downloads.** Every file has an exact URL (a versioned path or a
  commit revision) and a SHA-256. `fetch` rejects a file whose checksum
  differs, and the build commands re-verify their inputs and record URL and
  checksum in the output header.
- **License first.** Register a source in `source/source.go` and in
  `../DATA_LICENSES.md` before downloading anything. A source whose license
  isn't recorded can't be fetched.
- **Share-alike stays local.** Anything derived from TLA is CC BY-SA 4.0. The
  lexicon is written under `data/corpora/`, and `build-lexicon` refuses to
  write into `script/`, where it would be embedded and committed.

## How the sign list is chosen

Unikemet often lists several signs with the same sound value. For example,
both S29 and O34 have the value `s`. `build-signs` keeps one sign per value:
the Unikemet phonemogram (`kEH_Func` exactly `Phonemogram`, value a single
character) that occurs most often in the TLA hieroglyphs. A value whose
candidate signs never occur in TLA is skipped, not guessed. The result is
checked by `TestCommittedSignListIsReproducible` (CI job `data`): rebuilding
from the pinned files must reproduce the committed file byte for byte.

The committed file therefore records sign *frequencies* counted in TLA
(column `corpus_count`), but no TLA text. TODO(decision): confirm that
committing these counts is fine under CC BY-SA, or drop the column.
