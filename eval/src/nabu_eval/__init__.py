"""Evaluation metrics (stub).

- :func:`chrf`: character n-gram F-score for translation output
  (Popović, 2015, "chrF: character n-gram F-score for automatic MT
  evaluation", WMT).
- :func:`phoneme_error_rate`: edit distance between phoneme sequences divided
  by reference length, for G2P output.
"""

from __future__ import annotations

from collections.abc import Sequence

__all__ = ["chrf", "phoneme_error_rate"]


def chrf(
    hypotheses: Sequence[str],
    references: Sequence[str],
    *,
    char_order: int = 6,
    word_order: int = 0,
    beta: float = 2.0,
) -> float:
    """Corpus-level chrF (``word_order=2`` gives chrF++), on a 0-100 scale."""
    raise NotImplementedError("chrf is not implemented yet")


def phoneme_error_rate(
    hypotheses: Sequence[Sequence[str]],
    references: Sequence[Sequence[str]],
) -> float:
    """Total phoneme edit distance divided by total reference length.

    Each item is a sequence of phonemes (e.g. ``["ʕ", "n", "χ"]``), not a raw
    string, so multi-character phonemes are not split.
    """
    raise NotImplementedError("phoneme_error_rate is not implemented yet")
