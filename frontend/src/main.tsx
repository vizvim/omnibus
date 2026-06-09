import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { Providers } from "./lib/transport";
import "./index.css";

// Minimal App shell for plan 02-01 — proves the React app boots inside the Connect
// TransportProvider. The real Library/Search/SeriesDetail pages land in plan 02-05.
function App() {
  return (
    <main className="p-8">
      <h1 className="text-2xl font-semibold">omnibus</h1>
      <p className="text-gray-600">Foundation slice — UI lands in a later plan.</p>
    </main>
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("root element not found");
}

createRoot(rootElement).render(
  <StrictMode>
    <Providers>
      <App />
    </Providers>
  </StrictMode>,
);
