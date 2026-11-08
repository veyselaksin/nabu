# nabu-translate

**Status: stub.** Every function raises `NotImplementedError`.

Planned role: translate English into a *transliteration* of the target
language: Middle Egyptian first, then Sumerian and Hittite. The Go G2P engine
(`g2p/`) then converts that transliteration to IPA.

```python
from nabu_translate import translate, TranslationResult

result: TranslationResult = translate("life", lang="egy")
result.transliteration  # str, NFC-normalized scholarly transliteration
result.confidence  # float in [0, 1]; model confidence, not historical accuracy
result.unknown_words  # list[str]; source words that could not be translated
```

Open questions (to settle before implementation):

- Model choice. It must be under an OSI-approved, commercially usable
  license, and so must its weights.
- Training data. Corpus licenses (see `../DATA_LICENSES.md`) decide what the
  model can be trained on and whether its outputs carry share-alike terms.
- Must run on CPU for local development.
