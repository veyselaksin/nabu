// The ancient languages Nabu covers or plans to cover. A language is usable
// when the API offers a rule set for it (RulesetInfo.language); the others
// are listed as coming soon. Language codes are ISO 639-3.

export interface Language {
  code: string;
  name: string;
  /** Stage or period the rules and corpus cover. */
  period: string;
  /** The writing system, shown as the script card's title. */
  script: string;
  /** A sample sign in the language's script, in a font the page loads (or empty). */
  mark: string;
  /** Transliteration convention, shown under the transliteration field. */
  convention: string;
  /** Signs not on a keyboard that the transliteration uses. */
  keys: string[];
  /** Example transliterations (each must work with the shipped rule set). */
  examples: string[];
  /** Example English input for translation. */
  englishExamples: string[];
}

export const LANGUAGES: Language[] = [
  {
    code: "egy",
    name: "Egyptian",
    period: "Middle Egyptian",
    script: "Hieroglyphs",
    mark: "𓋹",
    convention: "Leiden Unified Transliteration, as in the TLA",
    keys: ["ꜣ", "ꞽ", "i̯", "ꜥ", "ḥ", "ḫ", "ẖ", "š", "ṯ", "ḏ", "q", "="],
    examples: ["ꜥnḫ", "ḥtp", "nfr", "ꜥnḫ ḥtp"],
    englishExamples: ["May the king live", "Bread and beer", "The sun rises"],
  },
  {
    code: "akk",
    name: "Akkadian",
    period: "Old Babylonian",
    script: "Cuneiform",
    mark: "",
    convention: "Assyriological transliteration",
    keys: ["š", "ṣ", "ṭ", "ḫ", "ā", "ē", "ī", "ū", "â", "ê", "î", "û"],
    examples: [],
    englishExamples: [],
  },
  {
    code: "sux",
    name: "Sumerian",
    period: "Classical Sumerian",
    script: "Cuneiform",
    mark: "",
    convention: "Assyriological transliteration",
    keys: ["š", "ŋ", "ḫ", "ř"],
    examples: [],
    englishExamples: [],
  },
  {
    code: "hit",
    name: "Hittite",
    period: "New Hittite",
    script: "Cuneiform",
    mark: "",
    convention: "Hittitological transliteration",
    keys: ["š", "ḫ"],
    examples: [],
    englishExamples: [],
  },
];
