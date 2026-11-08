import pytest

from nabu_tts import synthesize


def test_synthesize_is_stub():
    with pytest.raises(NotImplementedError):
        synthesize("ʕnχ", voice="default")
