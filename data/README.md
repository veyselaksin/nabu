# data/: download and build tools

Go module with one command, `nabu-data`. It downloads Nabu's upstream data
and builds derived files from it. **No corpus data is committed.** Downloads
go to `<root>/<source>/`, where `<root>` is `$NABU_DATA_DIR`, else
`$NABU_HOME/datasets`, else `$MODELS_ROOT/projects/nabu/datasets` when
`MODELS_ROOT` is set (a shared model drive; it must be mounted), else the
git-ignored `data/corpora/`, which is also excluded from Docker images.

```sh
go run ./data/cmd/nabu-data sources               # every source, its license and pinned files
go run ./data/cmd/nabu-data fetch unikemet tla    # download (make data-fetch)
go run ./data/cmd/nabu-data build-signs           # → script/signs/egy-unikemet-uniliterals.tsv (make signs)
go run ./data/cmd/nabu-data build-lexicon         # → <root>/tla/lexicon-egy.tsv (make lexicon)
go run ./data/cmd/nabu-data build-corpus          # → <root>/corpus/egy/ (make corpus)
```

| Source | What Nabu uses | License | Output |
| --- | --- | --- | --- |
| `unikemet`: Unicode Unikemet database 18.0.0 | sign values (`kEH_FVal`) and functions (`kEH_Func`) | Unicode-3.0 | sign list, **committed** (permissive) |
| `tla`: TLA Earlier Egyptian sentences, corpus v18 | aligned hieroglyphs + transliteration | CC BY-SA 4.0 | spelling lexicon, corpus, **local only** |
| `tla-late`, `tla-demotic`: TLA Late Egyptian v19, Demotic v18 | sentences with German translations | CC BY-SA 4.0 | corpus, **local only** |
| `aes`: Ancient Egyptian Sentences (BBAW 2018 extract) | sentences, lemma IDs, hieroglyphs (package `aes`) | CC BY-SA 4.0 | corpus, **local only** |
| `aed`: Ancient Egyptian Dictionary | lemmas with German/English meanings (package `aed`) | CC BY-SA 4.0 | corpus lexicon, **local only** |
| `coptic-dict`: Comprehensive Coptic Lexicon v1.2 | entries with EN/DE/FR glosses (package `coptic`) | CC BY-SA 4.0 | corpus lexicon, **local only** |
| `wiktionary-egy`: English Wiktionary, Egyptian entries | English glosses and labeled IPA for AED entries (package `wiktionary`) | CC BY-SA 4.0 | corpus lexicon, **local only** |
| `oraec-coptic-etymologies`: ORAEC Coptic etymologies | Coptic descendants of Egyptian lemmas (package `oraec`) | CC0 1.0 | corpus lexicon |
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
  lexicon is written under `<root>`, and `build-lexicon` refuses to
  write into `script/`, where it would be embedded and committed.

## The merged corpus

`build-corpus` (package `corpus`) writes `sentences.jsonl`, `lexicon.jsonl`
and `manifest.json` (sources, pinned files, counts) to `<root>/corpus/egy/`.
Every record names its source and ID.

- **Enriched dictionary entries.** An AED entry gets from Wiktionary an
  English gloss when it has none (`en_source` then says so) and its labeled
  IPA (`pronunciations`: "modern Egyptological" readings and dated
  reconstructions), and from ORAEC the Coptic words it survives as
  (`coptic`). Egyptian is full of homonyms, so a Wiktionary entry is joined
  only when it is the only AED word and the only Wiktionary word with that
  spelling and word class; roots and affixes are never joined. On the
  2026-11-16 data that joins 1,434 entries (218 gain English, 1,421 a
  pronunciation) and leaves 1,687 Wiktionary entries unused as homonyms.
  Where both sources have English, 87% of the joined pairs share a word
  stem, which is a floor for how often the join is right, not a review.
  1,407 entries have Coptic descendants.
- **Quality report.** `manifest.json` records per source what was dropped
  as duplicate or skipped and why, and per lexicon source how many entries
  have English, German, a pronunciation or no gloss at all; `build-corpus`
  prints it.
- **One convention.** AES uses the BBAW conventions of 2018.
  `aes.ConvertForm` converts them to the TLA v18 ones (`j`/`ı͗` → `ꞽ`,
  `,` → `.`, `≡` → `=`, `.pl` → `.PL`, lower case, editorial marks removed).
  On the 5,600 AES sentences that the TLA also has, 96.1% of 15,932 tokens
  convert to exactly the TLA form. The rest are different editorial
  readings. TLA Late Egyptian's `⸗` becomes `=`.
- **Intact sentences only**, like the TLA "premium" sets: AES sentences with
  lacunae (`...`, `_`) or uncertain readings (`?`, `⸮`) are skipped.
- **Deduplication.** TLA is read first (the newer edition). A later sentence
  with the same lemma IDs and translation, or the same transliteration and
  translation, is dropped. A TLA sentence that AES also has gets AES's text
  ID, so that train/test splits can keep whole texts together (`group`).
- **Stage.** TLA sets give `earlier`, `late` or `demotic`. AES gives `earlier`
  for Old/Middle Kingdom texts and leaves later texts unknown, because they
  may be in Middle Egyptian, Late Egyptian or Demotic.

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
