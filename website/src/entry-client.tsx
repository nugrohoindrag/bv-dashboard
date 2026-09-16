// Client entry: hydrate HTML hasil prerender (fallback ke render biasa saat dev tanpa SSR).
import React from "react";
import { createRoot, hydrateRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
// Urutan penting: font → token DS → palet BuildingVision → Tailwind (alias token)
import "@buildingvision/ui/bv/fonts.css";
import "@buildingvision/ui/tokens.css";
import "@buildingvision/ui/bv/palette.css";
import "./styles/site.css";
import { App } from "./App";

const root = document.getElementById("root")!;
const tree = (
  <React.StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </React.StrictMode>
);
if (root.hasChildNodes()) hydrateRoot(root, tree);
else createRoot(root).render(tree);
