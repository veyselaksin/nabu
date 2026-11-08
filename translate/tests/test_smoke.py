import pytest

from nabu_translate import SUPPORTED_LANGUAGES, TranslationResult, translate


def test_result_fields():
    r = TranslationResult(transliteration="ꜥnḫ", confidence=0.5)
    assert r.unknown_words == []
    assert "egy" in SUPPORTED_LANGUAGES


def test_confidence_range_checked():
    with pytest.raises(ValueError):
        TranslationResult(transliteration="", confidence=1.5)


def test_translate_is_stub():
    with pytest.raises(NotImplementedError):
        translate("life", "egy")
