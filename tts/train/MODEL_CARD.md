# Model card: Nabu TTS `${model_name}`

<!--
This file is the TEMPLATE. `nabu-tts-export` fills in the ${...} fields and
writes <model>.model-card.md next to the .onnx file. Complete every TODO by
hand before publishing a model.
-->
${synthetic_warning}
> [!IMPORTANT]
> **This voice does not reproduce how any ancient language sounded.** Nobody
> knows that, and there are no recordings. The model reads IPA. The IPA comes
> from a published scholarly *reconstruction*, applied by Nabu's rule files.
> The model learned to pronounce IPA symbols from recordings of **modern**
> languages ("phonetic donors", listed below). Its accent, rhythm, intonation
> and voice quality come from those modern speakers, not from antiquity.

## Summary

| | |
|---|---|
| File | `${onnx_file}` |
| Architecture | VITS (Kim, Kong & Son, 2021), multi-speaker, IPA code points as input tokens |
| Reference implementation | [jaywalnut310/vits](https://github.com/jaywalnut310/vits) (MIT), adapted in `tts/train/src/nabu_tts/vits/` |
| Training step | ${step} |
| Exported | ${exported_at} |
| Sample rate | ${sample_rate} Hz |
| Speakers | ${num_speakers} (pseudonymous Common Voice contributors) |
| Symbol table | `tts/symbols.txt`, sha256 `${symbols_sha256}` |
| ONNX parity check | ${parity} |
| License of these weights | TODO(decision): the code is Apache-2.0. Common Voice is CC0 and adds no terms. State the license chosen for the weights here. |

## Training data

Source: [Mozilla Common Voice](https://commonvoice.mozilla.org/) (CC0-1.0).
Common Voice's terms ask users not to try to identify the speakers. Speaker
labels in this model are one-way hashes of Common Voice's `client_id`.

Orthography → IPA labels were produced with [Epitran](https://github.com/dmort27/epitran)
(MIT). No GPL tools (e.g. espeak-ng) were used. Clips whose text Epitran could
not fully convert were dropped and counted; see each locale's `report.json`
from `nabu-tts-prepare`.

### Phonetic donor languages

| Language (CV locale) | Clips | Hours | Speakers |
|---|---:|---:|---:|
${language_rows}

Manifests:

${manifests}

TODO: Explain why these languages were chosen (link the `nabu-tts-coverage` report).
List which target IPA symbols had zero or low coverage.

## What this model is and is not

- It is an **IPA reader**. It is only as good as (a) the reconstruction in the
  rule file, (b) the rule file's faithfulness to that reconstruction, and
  (c) how well the donor recordings cover each IPA symbol.
- Every output must be labeled with the reconstruction it follows (the API
  returns it in `X-Nabu-Ruleset` / `X-Nabu-Reference`).
- It is **not** evidence for any pronunciation. Do not cite its audio as such.

## Known limitations

- **Accent transfer.** Symbols are pronounced the way the donor speakers
  pronounce them, including language-specific allophony and prosody.
- **Coverage gaps.** Symbols that are rare or absent in the donor data are
  pronounced poorly or as a nearby sound. TODO: list them from the coverage report.
- **Label noise.** Epitran's orthography → IPA conversion is rule-based and
  will mislabel some words (loanwords, names, irregular spellings). Scripts that
  leave short vowels unwritten were excluded unless stated otherwise above.
- **Prosody.** Punctuation is removed from training text, so the model has no
  phrase-level intonation cues. Stress and length are only modeled where the
  IPA marks them.
- **Speakers.** Voices are a small set of Common Voice contributors and
  reflect their recording conditions.
- TODO: anything observed in listening tests (`eval/samples/`).

## Evaluation

TODO: phoneme error rate (phoneme recognizer → PER against the input IPA) is
not implemented yet. Until then, evaluation is listening to the fixed samples in
`eval/samples/`, rendered with `make tts-samples`.
