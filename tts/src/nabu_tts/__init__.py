"""IPA → speech synthesis (stub)."""

from __future__ import annotations

__all__ = ["synthesize"]


def synthesize(ipa: str, voice: str) -> bytes:
    """Render ``ipa`` with ``voice`` and return a complete WAV file as bytes."""
    raise NotImplementedError("speech synthesis is not implemented yet")
