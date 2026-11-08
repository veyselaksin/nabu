import pytest

from nabu_eval import chrf, phoneme_error_rate


def test_metrics_are_stubs():
    with pytest.raises(NotImplementedError):
        chrf(["a"], ["a"])
    with pytest.raises(NotImplementedError):
        phoneme_error_rate([["a"]], [["a"]])
