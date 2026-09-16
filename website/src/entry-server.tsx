// Server entry (prerender): render satu route ke HTML + head tags (Website PRD §39 SEO).
import React from "react";
import { renderToString } from "react-dom/server";
import { StaticRouter } from "react-router";
import { App } from "./App";
import { HeadContext, renderHead, type HeadCollector } from "./lib/head";

export { ALL_ROUTES } from "./content/site";

export function render(url: string): { html: string; head: string } {
  const collector: HeadCollector = { current: null };
  const html = renderToString(
    <React.StrictMode>
      <HeadContext.Provider value={collector}>
        <StaticRouter location={url}>
          <App />
        </StaticRouter>
      </HeadContext.Provider>
    </React.StrictMode>,
  );
  const head = collector.current ? renderHead(collector.current) : "<title>BuildingVision</title>";
  return { html, head };
}
