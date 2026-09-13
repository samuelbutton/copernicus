import type { ReactNode } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import type { Frozen, Progress } from "./api";

export function Load<T>({
  query,
  name,
  children,
}: {
  query: UseQueryResult<T, Error>;
  name: string;
  children: (data: T) => ReactNode;
}) {
  if (query.isError)
    return (
      <div role="alert" className="notice">
        <p>{query.error.message}</p>
        <p>Previously loaded values are hidden until this read succeeds.</p>
        <button
          onClick={() => {
            void query.refetch();
          }}
        >
          Retry {name}
        </button>
      </div>
    );
  if (query.isPending)
    return (
      <p role="status" className="notice">
        Loading {name}…
      </p>
    );
  return (
    <>
      <div className="freshness">
        {query.isFetching
          ? "Checking for updates…"
          : `Last checked ${new Date(query.dataUpdatedAt).toLocaleTimeString()}`}{" "}
        · Updates every 5 seconds
      </div>
      {children(query.data)}
    </>
  );
}
export function State({ value }: { value: string }) {
  return (
    <span
      className={`state ${["FAIL", "ERROR", "REGRESSION", "RESOLUTION_FAILED"].includes(value) ? "negative" : ""}`}
    >
      {value.replaceAll("_", " ").toLowerCase()}
    </span>
  );
}
export function Totals({
  data,
}: {
  data: Omit<Progress, "executions" | "request_id">;
}) {
  return (
    <div className="totals">
      <p>
        <strong>
          {data.completed} of {data.total}
        </strong>{" "}
        tests completed
      </p>
      <progress
        aria-label="Tests completed"
        value={data.completed}
        max={Math.max(1, data.total)}
      />
      <p>
        {data.passed} passed · {data.failed} failed · {data.warnings} warnings ·{" "}
        {data.errors} errors
      </p>
      {!data.complete && (
        <p className="notice">
          Partial results: {data.incomplete} tests remain incomplete, including{" "}
          {data.resolution_failed} input failures.
        </p>
      )}
    </div>
  );
}
export function Inputs({ name, value }: { name: string; value: Frozen }) {
  return (
    <details>
      <summary>
        {name}: {value.id}
      </summary>
      <p className="hash">SHA256 {value.sha256}</p>
      <pre>{JSON.stringify(value.content, null, 2)}</pre>
    </details>
  );
}
