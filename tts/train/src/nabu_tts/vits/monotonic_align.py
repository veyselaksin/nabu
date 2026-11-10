"""Monotonic alignment search (Glow-TTS / VITS).

Nabu: numba port of upstream's Cython ``monotonic_align/core.pyx`` (same
algorithm, same results), so no C extension has to be compiled.
"""

import numba
import numpy as np
import torch


@numba.njit(cache=True, nogil=True)
def _maximum_path_each(path, value, t_y, t_x, max_neg_val=-1e9):
    index = t_x - 1
    for y in range(t_y):
        for x in range(max(0, t_x + y - t_y), min(t_x, y + 1)):
            if x == y:
                v_cur = max_neg_val
            else:
                v_cur = value[y - 1, x]
            if x == 0:
                if y == 0:
                    v_prev = 0.0
                else:
                    v_prev = max_neg_val
            else:
                v_prev = value[y - 1, x - 1]
            value[y, x] += max(v_prev, v_cur)

    for y in range(t_y - 1, -1, -1):
        path[y, index] = 1
        if index != 0 and (index == y or value[y - 1, index] < value[y - 1, index - 1]):
            index = index - 1


@numba.njit(cache=True, nogil=True, parallel=True)
def _maximum_path_batch(paths, values, t_ys, t_xs):
    for i in numba.prange(paths.shape[0]):
        _maximum_path_each(paths[i], values[i], t_ys[i], t_xs[i])


def maximum_path(neg_cent, mask):
    """neg_cent: [b, t_t, t_s]; mask: [b, t_t, t_s]. Returns the hard alignment path."""
    device = neg_cent.device
    dtype = neg_cent.dtype
    values = neg_cent.detach().cpu().numpy().astype(np.float32)
    paths = np.zeros(values.shape, dtype=np.int32)
    t_t_max = mask.sum(1)[:, 0].detach().cpu().numpy().astype(np.int32)
    t_s_max = mask.sum(2)[:, 0].detach().cpu().numpy().astype(np.int32)
    _maximum_path_batch(paths, values, t_t_max, t_s_max)
    return torch.from_numpy(paths).to(device=device, dtype=dtype)
