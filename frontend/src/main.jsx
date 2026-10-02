import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";

// Fonts are bundled with the app (no Google Fonts request), so OpScope
// also works on machines without internet access.
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";

import "./styles.css";
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
