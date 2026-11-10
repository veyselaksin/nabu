"""Linear and mel spectrograms for VITS training.

Nabu: replaces upstream ``mel_processing.py``. Same framing (reflect padding of
(n_fft - hop) / 2, center=False, Hann window, magnitude with +1e-6, log with
clamp 1e-5), but uses torch.stft(return_complex=True) and builds the mel
filterbank here instead of importing librosa. The filterbank follows the
Slaney-style mel scale with area normalization, which is what upstream got
from ``librosa.filters.mel`` defaults.
"""

import numpy as np
import torch

_windows = {}
_mel_bases = {}


def _hz_to_mel(f):
    f = np.asanyarray(f, dtype=np.float64)
    f_sp = 200.0 / 3
    mels = f / f_sp
    min_log_hz = 1000.0
    min_log_mel = min_log_hz / f_sp
    logstep = np.log(6.4) / 27.0
    return np.where(
        f >= min_log_hz, min_log_mel + np.log(np.maximum(f, 1e-10) / min_log_hz) / logstep, mels
    )


def _mel_to_hz(m):
    m = np.asanyarray(m, dtype=np.float64)
    f_sp = 200.0 / 3
    freqs = f_sp * m
    min_log_hz = 1000.0
    min_log_mel = min_log_hz / f_sp
    logstep = np.log(6.4) / 27.0
    return np.where(m >= min_log_mel, min_log_hz * np.exp(logstep * (m - min_log_mel)), freqs)


def mel_filterbank(sr, n_fft, n_mels, fmin=0.0, fmax=None):
    """[n_mels, n_fft // 2 + 1] triangular filters, Slaney mel scale and normalization."""
    if fmax is None:
        fmax = sr / 2.0
    fftfreqs = np.linspace(0, sr / 2.0, 1 + n_fft // 2)
    mel_f = _mel_to_hz(np.linspace(_hz_to_mel(fmin), _hz_to_mel(fmax), n_mels + 2))
    fdiff = np.diff(mel_f)
    ramps = mel_f[:, None] - fftfreqs[None, :]
    lower = -ramps[:-2] / fdiff[:-1, None]
    upper = ramps[2:] / fdiff[1:, None]
    weights = np.maximum(0, np.minimum(lower, upper))
    enorm = 2.0 / (mel_f[2 : n_mels + 2] - mel_f[:n_mels])
    weights *= enorm[:, None]
    return weights.astype(np.float32)


def _window(win_size, y):
    key = (win_size, y.dtype, y.device)
    if key not in _windows:
        _windows[key] = torch.hann_window(win_size).to(dtype=y.dtype, device=y.device)
    return _windows[key]


def spectrogram_torch(y, n_fft, sampling_rate, hop_size, win_size, center=False):
    """y: [b, t] in [-1, 1] -> linear magnitude [b, n_fft // 2 + 1, frames]."""
    pad = int((n_fft - hop_size) / 2)
    y = torch.nn.functional.pad(y.unsqueeze(1), (pad, pad), mode="reflect").squeeze(1)
    spec = torch.stft(
        y,
        n_fft,
        hop_length=hop_size,
        win_length=win_size,
        window=_window(win_size, y),
        center=center,
        pad_mode="reflect",
        normalized=False,
        onesided=True,
        return_complex=True,
    )
    return torch.sqrt(spec.real.pow(2) + spec.imag.pow(2) + 1e-6)


def spec_to_mel_torch(spec, n_fft, num_mels, sampling_rate, fmin, fmax):
    key = (n_fft, num_mels, sampling_rate, fmin, fmax, spec.dtype, spec.device)
    if key not in _mel_bases:
        _mel_bases[key] = torch.from_numpy(
            mel_filterbank(sampling_rate, n_fft, num_mels, fmin, fmax)
        ).to(dtype=spec.dtype, device=spec.device)
    mel = torch.matmul(_mel_bases[key], spec)
    return torch.log(torch.clamp(mel, min=1e-5))


def mel_spectrogram_torch(
    y, n_fft, num_mels, sampling_rate, hop_size, win_size, fmin, fmax, center=False
):
    spec = spectrogram_torch(y, n_fft, sampling_rate, hop_size, win_size, center)
    return spec_to_mel_torch(spec, n_fft, num_mels, sampling_rate, fmin, fmax)
