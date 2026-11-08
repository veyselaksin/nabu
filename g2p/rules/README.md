# G2P rule files

There is one YAML file per pronunciation reconstruction, named `<id>.yaml`.
Every file here is embedded into the Go binaries and validated by
`go test ./g2p/...`.

> Every rule must cite its source. Do not add a mapping from memory or
> intuition. If a value is not yet sourced, mark it `# TODO(source): …` and
> make sure the file's `name`/`notes` make clear that it is a placeholder.

## Schema

```yaml
id: egy-allen                 # required; lowercase kebab-case; must equal the file name
language: egy                 # required; ISO 639 code (egy, sux, hit, …)
name: Middle Egyptian (Allen) # required; human-readable
reference: "Allen, J. P. (2013). …"  # required; full citation
version: 0.1.0                # required; bump when outputs change
classes:                      # optional; named sets usable in contexts as {Name}
  V: [a, i, u]                #   names start with an uppercase letter
graphemes:                    # required; at least one rule
  - match: "ḫ"                # required; NFC; may be several characters
    ipa: "x"                  # required; may be "" to map explicitly to nothing
    context:                  # optional; at least one of before/after
      before: "{V}"           #   regex matched against the text to the LEFT  (anchored at its end)
      after: "(?:\\s|$)"      #   regex matched against the text to the RIGHT (anchored at its start)
notes: free text              # optional
```

Unknown keys are rejected.

## Matching semantics

- Input is NFC-normalized first, so `h` + U+032E and `ḫ` (U+1E2B) are the same.
- At each position the **longest** `match` wins (counted in code points). Ties
  are broken by file order, so put context-restricted rules *before* the
  general rule for the same grapheme.
- A rule never matches only part of a base character + combining-mark
  sequence.
- Contexts are [Go RE2 regular expressions](https://pkg.go.dev/regexp/syntax)
  evaluated over the whole (normalized) input, so use `^`, `$` and `\s` for
  word edges. `{Name}` expands to the members of class `Name`.
- Whitespace that no rule matches is copied through; any other uncovered
  character is an error.
