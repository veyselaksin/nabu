import numpy as np

from nabu_tts.audio import (
    QualityThresholds,
    measure,
    read_wav,
    reject_reason,
    trim_silence,
    write_wav,
)

SR = 16000
T = QualityThresholds()


def tone(seconds, amp=0.5, f=220.0):
    t = np.arange(int(SR * seconds)) / SR
    return (amp * np.sin(2 * np.pi * f * t)).astype(np.float32)


def with_silence(x, seconds=0.5, noise=0.0, seed=0):
    rng = np.random.default_rng(seed)
    pad = (noise * rng.standard_normal(int(SR * seconds))).astype(np.float32)
    return np.concatenate([pad, x, pad])


def test_clean_clip_passes():
    assert reject_reason(measure(with_silence(tone(1.0), noise=1e-4), SR, T), T) is None


def test_rejections():
    assert reject_reason(measure(with_silence(tone(1.0, amp=0.01)), SR, T), T) == "too_quiet"
    clipped = np.clip(tone(1.0, amp=3.0), -1, 1)
    assert reject_reason(measure(with_silence(clipped), SR, T), T) == "clipped"
    noisy = tone(1.0, amp=0.3) + 0.1 * np.random.default_rng(0).standard_normal(SR).astype(
        np.float32
    )
    q = measure(with_silence(noisy, noise=0.1), SR, T)
    assert q.clipped_fraction == 0
    assert reject_reason(q, T) == "noisy"


def test_trim_keeps_padding():
    x = with_silence(tone(1.0), seconds=1.0)
    y = trim_silence(x, SR, T)
    assert 1.0 <= len(y) / SR <= 1.0 + 2 * T.trim_pad_ms / 1000 + 0.05


def test_wav_round_trip(tmp_path):
    x = tone(0.1)
    write_wav(tmp_path / "a.wav", x, SR)
    y, sr = read_wav(tmp_path / "a.wav")
    assert sr == SR and len(y) == len(x)
    assert np.max(np.abs(x - y)) < 1e-4
