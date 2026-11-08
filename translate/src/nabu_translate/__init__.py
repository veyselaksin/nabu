"""English → ancient-language translation (stub).

The planned model translates English into a *transliteration* of the target
language (e.g. Middle Egyptian), which the Go G2P engine then turns into IPA.
"""

from __future__ import annotations

from dataclasses import dataclass, field

__all__ = ["SUPPORTED_LANGUAGES", "TranslationResult", "translate"]

#: ISO 639 codes Nabu intends to support, in planned order.
SUPPORTED_LANGUAGES: tuple[str, ...] = ("egy", "sux", "hit")


@dataclass(frozen=True, slots=True)
class TranslationResult:
    """Output of :func:`translate`."""

    #: Target-language text in scholarly transliteration (NFC-normalized).
    transliteration: str
    #: Model confidence in [0, 1]. Not a measure of historical accuracy.
    confidence: float
    #: Source words the model could not translate.
    unknown_words: list[str] = field(default_factory=list)

    def __post_init__(self) -> None:
        if not 0.0 <= self.confidence <= 1.0:
            raise ValueError(f"confidence must be in [0, 1], got {self.confidence}")


def translate(text: str, lang: str) -> TranslationResult:
    """Translate English ``text`` into ``lang`` (an ISO 639 code such as ``"egy"``)."""
    raise NotImplementedError("translation is not implemented yet")
