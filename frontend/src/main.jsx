import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";

// Fonts are bundled with the app (no Google Fonts request), so Opscope
// also works on machines without internet access. Both are variable fonts:
// one file per script covers every weight.
import "@fontsource-variable/manrope";
import "@fontsource-variable/jetbrains-mono";

import "./styles/tokens.css";
import "./styles/base.css";
import "./styles/legacy.css";
import "./theme.js"; // keeps the theme in step with the OS and other tabs
import App from "./App.jsx";
import { ClustersProvider } from "./clusters.jsx";

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <BrowserRouter>
      <ClustersProvider>
        <App />
      </ClustersProvider>
    </BrowserRouter>
  </StrictMode>,
);
