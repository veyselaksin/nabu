# nabu-eval

**Status: stub.** Every function raises `NotImplementedError`.

Planned role: shared metrics for measuring the translation and G2P stages.

| Function | Measures | Notes |
| --- | --- | --- |
| `chrf(hypotheses, references, *, char_order=6, word_order=0, beta=2.0)` | translation quality | Popović (2015). `word_order=2` gives chrF++. Should match a reference implementation such as sacreBLEU (Apache-2.0) to the reported precision. |
| `phoneme_error_rate(hypotheses, references)` | G2P quality | Levenshtein distance over phoneme sequences ÷ total reference length. Inputs are pre-tokenized phoneme lists. |

The package name is `nabu_eval`, not `eval`, so it doesn't shadow Python's
built-in `eval`.
