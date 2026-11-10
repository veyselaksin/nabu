import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
// Only the hieroglyph subset (OFL-1.1); text uses the system font.
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
