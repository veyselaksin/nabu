from nabu_data import cdli, oracc, tla


def test_each_script_prints_url_and_license(capsys):
    for module in (oracc, cdli, tla):
        assert module.main() == 0
        out = capsys.readouterr().out
        assert module.SOURCE.url in out
        assert module.SOURCE.license in out
        assert "nothing was fetched" in out
