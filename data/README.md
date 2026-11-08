# nabu-data

**Status: stub.** The scripts only print each source's URL and license.

Planned role: one download script and one cleaning script per corpus. The
cleaning step will NFC-normalize transliterations to match `g2p/`.

```sh
uv run nabu-data-oracc   # Sumerian (ORACC)
uv run nabu-data-cdli    # Sumerian (CDLI)
uv run nabu-data-tla     # Middle Egyptian (TLA)
```

Rules:

- Never commit corpus data. Downloads go to `data/corpora/` (or
  `$NABU_DATA_DIR`), which is git-ignored.
- Before implementing a downloader, verify the source's license and record it
  in `../DATA_LICENSES.md` and in the module's `SOURCE`. Note any share-alike
  terms, since they may apply to models trained on the data.
- Respect each site's terms of use and rate limits. Prefer official bulk
  exports to scraping.
- No Hittite source has been chosen yet.
