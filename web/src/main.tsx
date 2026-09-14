import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import "./styles/theme.css";
import "./lib/i18n";
import { AuthProvider } from "./lib/auth";
import { ToastProvider } from "./components/bv/common";
import { App } from "./app/App";

const qc = new QueryClient({ defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } } });

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <ToastProvider>
          <App />
        </ToastProvider>
      </AuthProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
