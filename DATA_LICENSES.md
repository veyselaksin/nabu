# Data licenses

This repository contains **no corpus data**, only scripts (`data/`) that
download it into the git-ignored `data/corpora/` directory. This table records
the terms of each source so that we know what a model trained on it may be
used for.

> **Status:** Unikemet and TLA have been verified (2026-11-10, from the
> license pages linked below). The cuneiform sources are still `TODO(source)`.
> Before writing a downloader for a source, check its license on the source's
> own site, fill in its row with a link to that license page, and register it
> in `data/source/source.go` (unverified sources can't be fetched).

| Source | URL | License | Share-alike? | Notes |
| --- | --- | --- | --- | --- |
| ORACC: Open Richly Annotated Cuneiform Corpus (Sumerian) | https://oracc.museum.upenn.edu/ | TODO(source): verify | TODO(source): verify | Terms may differ per ORACC sub-project; record each project used. |
| CDLI: Cuneiform Digital Library Initiative (Sumerian) | https://cdli.mpiwg-berlin.mpg.de/ | TODO(source): verify | TODO(source): verify | Catalogue data, transliterations and images may be licensed separately. |
| TLA: Thesaurus Linguae Aegyptiae, Original Earlier Egyptian sentences, corpus v18, premium (Middle Egyptian) | https://huggingface.co/datasets/thesaurus-linguae-aegyptiae/tla-Earlier_Egyptian_original-v18-premium (pinned to revision `a7ba9e9`) | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/), per the dataset card | **Yes** | Used to build the hieroglyph spelling lexicon, to choose between candidate signs, and as the retrieval corpus and vocabulary check for translation (`translate/`, read at startup, never written out). **Decision:** the lexicon is built locally, never committed or baked into images, and every API response that uses it carries the attribution below; `/v1/translate` also returns the TLA sentences it gave the model, with the same attribution. TODO(decision): if a public deployment serves TLA-derived spellings or translations produced with TLA examples, decide how share-alike applies to them. Also confirm that committing per-sign frequency counts (column `corpus_count` in `script/signs/egy-unikemet-uniliterals.tsv`) is acceptable. Required attribution (from the dataset card): "Thesaurus Linguae Aegyptiae, Original Earlier Egyptian sentences, corpus v18, premium, v1.1, 2/16/2024, ed. by Tonio Sebastian Richter & Daniel A. Werning on behalf of the Berlin-Brandenburgische Akademie der Wissenschaften and Hans-Werner Fischer-Elfert & Peter Dils on behalf of the Sächsische Akademie der Wissenschaften zu Leipzig." |
| Unicode Unikemet database 18.0.0 (Egyptian hieroglyph properties) | https://www.unicode.org/Public/18.0.0/ucd/Unikemet.txt | [Unicode-3.0](https://www.unicode.org/license.txt) (OSI-approved) | No | Sign values used for phonetic spellings. The derived sign list `script/signs/egy-unikemet-uniliterals.tsv` is committed and embedded, with the license next to it (`script/signs/LICENSE-unicode.txt`). |
| Hittite source | (none chosen) | — | — | TODO(source): identify a Hittite corpus with usable terms. |
| Mozilla Common Voice (TTS training audio) | https://commonvoice.mozilla.org/ | CC0-1.0 | No | Speech recordings, not downloaded by Nabu: get each locale from Mozilla, accepting its terms, which include not trying to identify speakers. `tts/train` hashes speaker ids. TODO(source): record the release version(s) used for each published model in its model card. |
| Epitran map tables (TTS labels) | https://github.com/dmort27/epitran | MIT | No | Orthography → IPA rules, installed as a Python dependency, not data we ship. |

## Policy

- Accept a source only if its terms allow commercial use. Sources marked
  non-commercial (NC) or no-derivatives (ND) are rejected.
- Share-alike sources (e.g. CC BY-SA) are allowed only after deciding how the
  share-alike terms apply to derived datasets and trained models. Record that
  decision in the Notes column.
- Keep attribution requirements here and surface them wherever outputs derived
  from the data are shown.
