import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Fonts (all OFL-1.1): Inter for the interface, Noto Serif for transliteration
// and IPA (it covers the Egyptological yod ꞽ U+A7BD and the combining breve of
// i̯, which Charis SIL's web subsets lack), and only the hieroglyph subset of
// Noto Sans Egyptian Hieroglyphs.
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "@fontsource/noto-serif/400.css";
import "@fontsource/noto-serif/400-italic.css";
import "@fontsource/noto-serif/700.css";
import "@fontsource/noto-sans-egyptian-hieroglyphs/egyptian-hieroglyphs-400.css";
import { App } from "./App";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root element missing");

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
