# Sign lists and conventions

These files are embedded into the Go binaries (and a future WASM build) by
`embed.go`.

| File | What | Edit how |
| --- | --- | --- |
| `egy-unikemet-uniliterals.tsv` | one Unicode hieroglyph per one-consonant (uniliteral) value | **generated**: `make signs` (see `data/README.md`) |
| `egy-conventions.yaml` | separators that aren't written (`.`, `=`, `-`) and aliases between transliteration conventions (`q` → `ḳ`) | by hand; every entry needs a `note` citing why |
| `LICENSE-unicode.txt` | Unicode License v3, which covers the sign list | – |

The sign list is derived from the Unicode Unikemet database (© Unicode, Inc.,
Unicode License v3). Its header records the exact upstream file and checksum.

## What the sign list can and can't do

It spells a word **sign by sign**, which is not how Egyptian was written.
Scribes combined multi-consonant signs, phonetic complements and classifiers.
The renderer labels such words `phonetic`. Attested spellings come only from
a lexicon built from a corpus (`make lexicon`, TLA, CC BY-SA 4.0), which is
loaded at runtime and never embedded.

Not covered yet. Each of these is a `TODO(decision)` in `egy-conventions.yaml`,
and until it's decided, rendering fails with "no sign spells …":

- `z`: TLA writes it, but Unikemet gives both S29 and O34 the value `s`.
- `j`, `i`, `ỉ`, `i̯`: other conventions' spellings of yod. Unikemet and TLA use `ꞽ`.
- `ẖ`: Unikemet has no uniliteral phonemogram with this value.
- Multi-consonant signs: not used, even where Unikemet has them.
