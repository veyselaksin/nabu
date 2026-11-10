"""VITS (Kim, Kong & Son, 2021), adapted from https://github.com/jaywalnut310/vits.

Upstream commit 2e561ba58618d021b5b8323d3765880f7e0ecfdb, MIT License,
Copyright (c) 2021 Jaehyeon Kim; see LICENSE in this directory.

The code is reformatted with ruff (4-space indents); otherwise upstream style is
kept. Changes from upstream (each marked with a "Nabu:" comment where it touches code):
- relative imports; monotonic alignment in numba instead of Cython (monotonic_align.py);
- mel/linear spectrograms without librosa, using torch.stft(return_complex=True) (mel.py);
- the speaker embedding exists whenever n_speakers >= 1;
- relative-position padding computed with tensor ops during ONNX export, so the
  exported graph accepts any input length.
"""
