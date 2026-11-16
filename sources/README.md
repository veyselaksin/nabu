# Sources: research summary (2026-11-16)

`registry.yaml` in this directory is the record: one entry per source that was
evaluated, with its license, the evidence for that license, access method,
reliability tier and a decision. This page summarizes it. Counts: 43 sources,
26 ingest, 9 reference-only, 4 hold, 4 reject.

Licenses were read on 2026-11-16 from the page or file named in each entry's
`license_evidence`. Nothing here was assumed from memory. Where the evidence
is weak or contradictory the entry says so.

## What changed the plan

1. **There is an open, checkable basis for vowels.** Three sources together
   give pronunciation evidence that the placeholder rule file lacks:
   - ORAEC's Coptic etymology map (CC0) links 1,695 Egyptian lemmas to their
     Coptic descendants. Every one of them joins to the local AED lexicon and
     every Coptic ID joins to the local Coptic lexicon.
   - English Wiktionary's Egyptian entries (CC BY-SA 4.0 or GFDL) carry IPA
     that is labeled per entry: 4,367 "modern Egyptological" readings, and
     about 980 reconstructions for each of Old, Middle and Late Egyptian.
   - Peust (1999), free to read at Heidelberg, is the reference to cite for
     consonant values.

   This supports three explicit conventions instead of one unsourced file:
   the Egyptological reading (rule-based, complete, and openly a modern
   convention), a Coptic-informed reading (about 1,700 lemmas), and a
   reconstructed reading (about 1,000 words, community-edited, to be shown
   with its label).
2. **English is still the gap for Egyptian.** No openly licensed sentence
   corpus pairs Middle Egyptian with English. The TLA and AES are German.
   The UD treebank (3,764 sentences) has stage labels and syntax but no
   translations. Nederhof's St Andrews texts would fill the gap, but the page
   states no license, so they are on hold. English exists only at word level:
   AED (15,875 lemmas) and Wiktionary (about 4,100 words).
3. **Published scores on TLA data may be inflated.** A 2026 preprint argues
   that formulaic repetition lets models memorize, and releases a
   decontaminated test set. Not yet read in full; it has to be before Nabu's
   own test set is fixed.
4. **A permissive pretrained TTS exists.** IMS Toucan (code and weights
   Apache-2.0) is built for languages without training data. MMS-TTS and
   F5-TTS are non-commercial and XTTS has a custom license: rejected.
5. **Cuneiform and Hittite are better supplied than Egyptian for English.**
   ORACC's JSON is CC0 and includes English translations; TLHdig covers most
   published Hittite tablets (transliteration only).

## Decisions by language

### Egyptian

| Source | License | Tier | Decision |
| --- | --- | --- | --- |
| TLA Earlier / Late / Demotic (Hugging Face) | CC BY-SA 4.0 | primary edition | ingest (in use) |
| AES, AED | CC BY-SA 4.0 | primary edition | ingest (in use) |
| Coptic Dictionary Online lexicon | CC BY-SA 4.0 | peer-reviewed | ingest (in use) |
| Unikemet | Unicode-3.0 | primary edition | ingest (in use) |
| ORAEC Coptic etymologies | CC0 | community | ingest (new) |
| ORAEC corpus raw data (text metadata) | CC BY-SA 4.0 / CC0 | primary edition | ingest (new) |
| UD Egyptian/Pre-Coptic treebank | CC BY-SA 4.0 | peer-reviewed | ingest (new) |
| Wiktionary Egyptian, Coptic (kaikki.org) | CC BY-SA 4.0 or GFDL | community | ingest (new) |
| UD Coptic-Scriptorium | CC BY 4.0 | peer-reviewed | ingest (new) |
| Ramsès transliteration data (Zenodo) | CC BY 4.0 | peer-reviewed | ingest (new) |
| PHOIBLE | CC BY 4.0 | peer-reviewed | ingest (new, for TTS donor choice) |
| St Andrews texts (Nederhof) | not stated | peer-reviewed | hold: ask the author |
| Ramsès Online | not stated | primary edition | reference only |
| TLA website | CC BY-SA 4.0 | primary edition | reference only (use the dumps) |
| Peust 1999; Allen 2013 | unclear; all rights reserved | peer-reviewed | reference only (cite) |

**Yale and Michigan.** No openly licensed Egyptian dataset from either
institution was found, in this pass or the earlier one. They are not in the
registry because there is no concrete resource to record. If you have a URL,
it gets an entry and a license check like every other source.

### Sumerian and Akkadian

| Source | License | Tier | Decision |
| --- | --- | --- | --- |
| ORACC JSON (ePSD2, ETCSRI, RINAP, RIAo, RIBo, SAAo, DCCLT, BLMS, Rīm-Anum) | CC0 | primary edition | ingest |
| eBL transliterated fragments (Zenodo, 2023) | CC BY 4.0 | primary edition | ingest |
| MTAAC gold corpus | CC0 | peer-reviewed | ingest |
| UD Akkadian PISANDUB, RIAO, MCONG | CC BY-SA 4.0 / 3.0 / 4.0 | peer-reviewed | ingest |
| Wiktionary Akkadian, Sumerian | CC BY-SA 4.0 or GFDL | community | ingest |
| CDLI | no standard license | primary edition | hold: ask CDLI |
| Akkademia | MIT | peer-reviewed | hold: 3.4 GB, ORACC-derived |
| ETCSL | CC BY-NC-SA 3.0 | primary edition | reject (non-commercial) |

### Hittite

| Source | License | Tier | Decision |
| --- | --- | --- | --- |
| TLHdig Beta 0.3 (Zenodo) | CC BY 4.0 on Zenodo; the project site says CC BY-SA | primary edition | ingest, treated as share-alike |
| UD Hittite-HitTB | CC BY-SA 4.0 | peer-reviewed | ingest |
| Wiktionary Hittite | CC BY-SA 4.0 or GFDL | community | ingest |
| eDiAna | CC BY-SA 4.0 | peer-reviewed | reference only (no bulk access) |

No openly licensed Hittite dictionary with English glosses was found other
than Wiktionary. The Chicago Hittite Dictionary and the CAD for Akkadian are
free to read but copyrighted; they were not evaluated as data sources.

## Consensus, dispute, reconstruction

- **Consensus:** the consonantal skeleton of Egyptian words as transliterated,
  and that Egyptian script does not write vowels.
- **Convention, not a claim about antiquity:** the Egyptological reading
  (inserting *e*, reading ꜣ and ꜥ as *a*). Useful because it is complete and
  nobody mistakes it for the real thing once it is labeled.
- **Reconstructed and disputed:** vowel qualities and stress before Coptic,
  and several consonant values (ꜣ, ꜥ, ḏ, ṯ, the emphatics). Reconstructions
  rest on Coptic, cuneiform and Greek transcriptions and comparative
  evidence, and scholars differ. Any reconstructed output keeps the name of
  the reconstruction it follows.

## Open checks

| What | How to verify |
| --- | --- |
| License of Nederhof's texts | Ask M.-J. Nederhof (St Andrews). |
| CDLI transliterations for commercial reuse | Ask CDLI; the terms page speaks only of "fair academic practice". |
| TLHdig: CC BY or CC BY-SA | Ask tlhdig@uni-wuerzburg.de which statement governs. |
| Peust 1999 reuse terms | Check the rights statement on the Heidelberg record page. |
| Toucan: IPA input without espeak-ng, size, Apple Silicon | Read the code and run it; not done yet. |
| The two recent Egyptian MT papers | Read them in full; so far only seen in search results. |
| kaikki.org per-language files | Marked deprecated upstream; move to the all-languages dump (2.9 GB) or the Wiktionary dump when they disappear. |
