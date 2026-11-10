"""Offline tooling for Nabu's IPA-input VITS voice.

Speech synthesis at request time is done in Go (package tts, ONNX Runtime);
this package only prepares data, trains and exports. Heavy dependencies are
extras: ``nabu-tts[data]`` and ``nabu-tts[train]``.
"""

from __future__ import annotations

__all__ = ["__version__"]

__version__ = "0.1.0"
