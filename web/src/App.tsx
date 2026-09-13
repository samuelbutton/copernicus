import { useEffect, useRef } from "react";
import { CreateRequest } from "./CreateRequest";
import { Comparison } from "./Comparison";
import { Execution } from "./Execution";
import { Requests, RequestView } from "./Requests";
import { useRoute, link } from "./navigation";
import { id } from "./api";
export function App() {
  const route = useRoute(),
    view = route.get("view") ?? "requests",
    routeKey = route.toString();
  const main = useRef<HTMLElement>(null);
  useEffect(() => {
    main.current?.focus();
  }, [routeKey]);
  const invalid = [
    "id",
    "execution",
    "baseline",
    "candidate",
    "analysis",
    "baseline_analysis",
    "candidate_analysis",
  ].some((key) => {
    const value = route.get(key);
    return value !== null && value !== "" && !id.safeParse(value).success;
  });
  return (
    <>
      <a
        className="skip-link"
        href="#main"
        onClick={(event) => {
          event.preventDefault();
          main.current?.focus();
        }}
      >
        Skip to content
      </a>
      <header className="page-header">
        <a className="wordmark" href={link({ view: "requests" })}>
          Copernicus
        </a>
        <nav aria-label="Main navigation">
          <a
            aria-current={
              view === "requests" || view === "request" ? "page" : undefined
            }
            href={link({ view: "requests" })}
          >
            Requests
          </a>
          <a
            aria-current={view === "compare" ? "page" : undefined}
            href={link({ view: "compare" })}
          >
            Compare
          </a>
        </nav>
        <span className="eyebrow">Local simulation review</span>
      </header>
      <main id="main" ref={main} tabIndex={-1}>
        {invalid ? (
          <>
            <h1>Invalid review link</h1>
            <p>
              Return to <a href={link({ view: "requests" })}>your requests</a>{" "}
              and choose a saved item.
            </p>
          </>
        ) : view === "requests" ? (
          <Requests />
        ) : view === "create" ? (
          <CreateRequest />
        ) : view === "request" ? (
          <RequestView
            key={routeKey}
            id={route.get("id") ?? ""}
            analysis={route.get("analysis") || "original"}
          />
        ) : view === "compare" ? (
          <Comparison
            key={routeKey}
            baseline={route.get("baseline") ?? ""}
            candidate={route.get("candidate") ?? ""}
            after={route.get("after") ?? ""}
            baselineAnalysis={route.get("baseline_analysis") || "original"}
            candidateAnalysis={route.get("candidate_analysis") || "original"}
          />
        ) : view === "execution" ? (
          <Execution route={route} />
        ) : (
          <>
            <h1>Page unavailable</h1>
            <a href={link({ view: "requests" })}>Return to requests</a>
          </>
        )}
      </main>
      <footer className="page-footer">
        Synthetic driving tests on your own computer. Scores describe this
        example, not real-world driving safety.
      </footer>
    </>
  );
}
