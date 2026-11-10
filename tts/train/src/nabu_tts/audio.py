"""Audio I/O and quality checks for data preparation and training.

Decoding uses miniaudio (pyminiaudio, MIT), resampling uses scipy (BSD-3)
and WAV files are read/written with the standard library. ffmpeg and
libsndfile are deliberately not used (both LGPL/GPL).
"""

from __future__ import annotations

import math
import wave
from dataclasses import dataclass
from fractions import Fraction
from pathlib import Path

import numpy as np


def decode_file(path: str | Path) -> tuple[np.ndarray, int]:
    """Decode an audio file (mp3, wav, flac, ogg/vorbis) to mono float32 in [-1, 1]."""
    import miniaudio

    decoded = miniaudio.decode_file(
        str(path), output_format=miniaudio.SampleFormat.FLOAT32, nchannels=1
    )
    samples = np.frombuffer(decoded.samples, dtype=np.float32).copy()
    return samples, decoded.sample_rate


def resample(x: np.ndarray, sr_in: int, sr_out: int) -> np.ndarray:
    """Polyphase resampling (anti-aliased)."""
    if sr_in == sr_out:
        return x
    from scipy.signal import resample_poly

    frac = Fraction(sr_out, sr_in)
    return resample_poly(x, frac.numerator, frac.denominator).astype(np.float32)


def write_wav(path: str | Path, x: np.ndarray, sr: int) -> None:
    """Write mono 16-bit PCM."""
    pcm = (np.clip(x, -1.0, 1.0) * 32767.0).round().astype("<i2")
    with wave.open(str(path), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(sr)
        w.writeframes(pcm.tobytes())


def read_wav(path: str | Path) -> tuple[np.ndarray, int]:
    """Read mono 16-bit PCM written by write_wav, as float32 in [-1, 1]."""
    with wave.open(str(path), "rb") as w:
        if w.getnchannels() != 1 or w.getsampwidth() != 2:
            raise ValueError(f"{path}: expected mono 16-bit PCM")
        sr = w.getframerate()
        data = w.readframes(w.getnframes())
    return np.frombuffer(data, dtype="<i2").astype(np.float32) / 32768.0, sr


@dataclass(frozen=True)
class QualityThresholds:
    # Fraction of samples at or above clip_level counts as clipping.
    clip_level: float = 0.999
    max_clipped_fraction: float = 0.001
    # Rough SNR estimate: loud-frame RMS vs quiet-frame RMS (percentiles).
    min_snr_db: float = 15.0
    # Reject near-silent recordings.
    min_peak: float = 0.05
    # Trimming: frames quieter than peak RMS minus trim_db are treated as silence.
    trim_db: float = 40.0
    trim_pad_ms: float = 100.0


@dataclass(frozen=True)
class Quality:
    peak: float
    clipped_fraction: float
    snr_db: float


def _frame_rms(x: np.ndarray, frame: int) -> np.ndarray:
    n = len(x) // frame
    if n == 0:
        return np.array([math.sqrt(float(np.mean(x**2))) if len(x) else 0.0])
    frames = x[: n * frame].reshape(n, frame)
    return np.sqrt(np.mean(frames**2, axis=1))


def measure(x: np.ndarray, sr: int, t: QualityThresholds) -> Quality:
    """Heuristic quality measures. SNR is a percentile estimate, not a true SNR."""
    peak = float(np.max(np.abs(x))) if len(x) else 0.0
    clipped = float(np.mean(np.abs(x) >= t.clip_level)) if len(x) else 0.0
    rms = _frame_rms(x, max(1, int(sr * 0.02)))
    loud = float(np.percentile(rms, 90))
    quiet = float(np.percentile(rms, 10))
    snr = 20.0 * math.log10(max(loud, 1e-9) / max(quiet, 1e-9))
    return Quality(peak=peak, clipped_fraction=clipped, snr_db=snr)


def reject_reason(q: Quality, t: QualityThresholds) -> str | None:
    if q.peak < t.min_peak:
        return "too_quiet"
    if q.clipped_fraction > t.max_clipped_fraction:
        return "clipped"
    if q.snr_db < t.min_snr_db:
        return "noisy"
    return None


def trim_silence(x: np.ndarray, sr: int, t: QualityThresholds) -> np.ndarray:
    """Cut leading/trailing frames more than trim_db below the loudest frame."""
    frame = max(1, int(sr * 0.02))
    rms = _frame_rms(x, frame)
    if len(rms) == 0 or rms.max() <= 0:
        return x
    threshold = rms.max() * 10 ** (-t.trim_db / 20.0)
    voiced = np.nonzero(rms >= threshold)[0]
    if len(voiced) == 0:
        return x
    pad = int(sr * t.trim_pad_ms / 1000.0)
    start = max(0, voiced[0] * frame - pad)
    end = min(len(x), (voiced[-1] + 1) * frame + pad)
    return x[start:end]
