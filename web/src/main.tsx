import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Component, StrictMode } from "react";
import { createRoot } from "react-dom/client";
import type { ReactNode } from "react";
import { App } from "./App";
import "./styles.css";

const client = new QueryClient();
class ErrorBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  override state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  override render() {
    return this.state.failed ? (
      <main>
        <h1>The review page could not open</h1>
        <p>Your saved requests are still available.</p>
        <button onClick={() => window.location.reload()}>Reload page</button>
      </main>
    ) : (
      this.props.children
    );
  }
}
const root = document.getElementById("root");
if (root === null) {
  throw new Error("The page is missing its content container.");
}
createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
