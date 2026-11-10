# Data licenses

This repository contains **no corpus data**, only scripts (`data/`) that
download it into the git-ignored `data/corpora/` directory. This table records
the terms of each source so that we know what a model trained on it may be
used for.

> **Status: nothing below has been verified yet.** Every license cell is a
> `TODO(source)`. Before writing a downloader for a source, check its license
> on the source's own site, fill in the row with a link to that license page,
> and update the matching `SOURCE` in `data/src/nabu_data/<source>.py`.

| Source | URL | License | Share-alike? | Notes |
| --- | --- | --- | --- | --- |
| ORACC: Open Richly Annotated Cuneiform Corpus (Sumerian) | https://oracc.museum.upenn.edu/ | TODO(source): verify | TODO(source): verify | Terms may differ per ORACC sub-project; record each project used. |
| CDLI: Cuneiform Digital Library Initiative (Sumerian) | https://cdli.mpiwg-berlin.mpg.de/ | TODO(source): verify | TODO(source): verify | Catalogue data, transliterations and images may be licensed separately. |
| TLA: Thesaurus Linguae Aegyptiae (Middle Egyptian) | https://thesaurus-linguae-aegyptiae.de/ | TODO(source): verify | TODO(source): verify | Identify the canonical bulk export and its license. |
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
