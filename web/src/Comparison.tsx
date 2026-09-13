import { AnalysisChoice } from "./Analysis";
import { useState } from "react";
import { comparisonSchema, useLive, useRequests } from "./api";
import type { Entry } from "./api";
import { Load, State, Totals } from "./components";
import { link, navigate } from "./navigation";

export function Comparison({
  baseline,
  candidate,
  after,
  baselineAnalysis,
  candidateAnalysis,
}: {
  baseline: string;
  candidate: string;
  after: string;
  baselineAnalysis: string;
  candidateAnalysis: string;
}) {
  const requests = useRequests();
  const [left, setLeft] = useState(baseline);
  const [right, setRight] = useState(candidate);
  const [leftAnalysis, setLeftAnalysis] = useState(baselineAnalysis);
  const [rightAnalysis, setRightAnalysis] = useState(candidateAnalysis);
  return (
    <>
      <p className="eyebrow">Baseline → candidate</p>
      <h1>What changed?</h1>
      <p className="lead">
        Compare the same tests under two braking rules. Open a test to inspect
        the evidence behind its scores.
      </p>
      <Load query={requests} name="request choices">
        {(ids) =>
          ids.length === 0 ? (
            <p className="notice">
              Create a request before comparing results.{" "}
              <a href={link({ view: "create" })}>Create request</a>
            </p>
          ) : (
            <form
              className="compare-form"
              onSubmit={(event) => {
                event.preventDefault();
                navigate({
                  view: "compare",
                  baseline: left,
                  candidate: right,
                  baseline_analysis: leftAnalysis,
                  candidate_analysis: rightAnalysis,
                });
              }}
            >
              <fieldset>
                <legend>Baseline</legend>
                <label>
                  Baseline request
                  <select
                    required
                    value={left}
                    onChange={(event) => {
                      setLeft(event.target.value);
                      setLeftAnalysis("original");
                    }}
                  >
                    <option value="">Choose baseline</option>
                    {ids.map((id) => (
                      <option key={id}>{id}</option>
                    ))}
                  </select>
                </label>
                {left && (
                  <AnalysisChoice
                    requestID={left}
                    label="Baseline scores"
                    value={leftAnalysis}
                    onChange={setLeftAnalysis}
                  />
                )}
              </fieldset>
              <fieldset>
                <legend>Candidate</legend>
                <label>
                  Candidate request
                  <select
                    required
                    value={right}
                    onChange={(event) => {
                      setRight(event.target.value);
                      setRightAnalysis("original");
                    }}
                  >
                    <option value="">Choose candidate</option>
                    {ids.map((id) => (
                      <option key={id}>{id}</option>
                    ))}
                  </select>
                </label>
                {right && (
                  <AnalysisChoice
                    requestID={right}
                    label="Candidate scores"
                    value={rightAnalysis}
                    onChange={setRightAnalysis}
                  />
                )}
              </fieldset>
              <button className="primary" type="submit">
                Compare requests
              </button>
            </form>
          )
        }
      </Load>
      {baseline && candidate ? (
        <Report
          baseline={baseline}
          candidate={candidate}
          after={after}
          baselineAnalysis={baselineAnalysis}
          candidateAnalysis={candidateAnalysis}
        />
      ) : (
        <p className="notice">
          Choose both requests to start the review. You can compare incomplete
          requests.
        </p>
      )}
    </>
  );
}
function Entries({
  entries,
  requestID,
  label,
  analysis,
  context,
}: {
  entries: Entry[];
  requestID: string;
  label: string;
  analysis: string;
  context: Record<string, string>;
}) {
  if (entries.length === 0) return <p>Not selected</p>;
  return (
    <>
      {entries.map((entry) => (
        <p key={entry.execution_id}>
          <a
            href={link({
              ...context,
              view: "execution",
              id: requestID,
              execution: entry.execution_id,
              analysis,
            })}
          >
            {label}: {entry.test_id}
          </a>
          <br />
          <State value={entry.state} />
        </p>
      ))}
    </>
  );
}
function Report({
  baseline,
  candidate,
  after,
  baselineAnalysis,
  candidateAnalysis,
}: {
  baseline: string;
  candidate: string;
  after: string;
  baselineAnalysis: string;
  candidateAnalysis: string;
}) {
  const query = useLive(
    `/api/comparisons?${new URLSearchParams({ baseline, candidate, after, baseline_analysis: baselineAnalysis, candidate_analysis: candidateAnalysis, limit: "25" })}`,
    comparisonSchema,
  );
  const context = {
    baseline,
    candidate,
    after,
    baseline_analysis: baselineAnalysis,
    candidate_analysis: candidateAnalysis,
  };
  return (
    <Load query={query} name="comparison">
      {(data) => {
        const report = data.comparison;
        return (
          <section aria-label="Comparison results">
            <div className="sides">
              {[report.baseline, report.candidate].map((side, index) => (
                <section key={index}>
                  <h2>
                    {index === 0 ? "Baseline" : "Candidate"}:{" "}
                    <a
                      href={link({
                        view: "request",
                        id: side.request_id,
                        analysis: side.analysis_selection,
                      })}
                    >
                      {side.request_id}
                    </a>
                  </h2>
                  <p>Selected scores: {side.analysis_selection}</p>
                  <Totals data={side} />
                </section>
              ))}
            </div>
            <p className="notice">
              <strong>{report.counts.regressions} metric regressions</strong> ·{" "}
              {report.counts.improvements} improvements ·{" "}
              {report.counts.unchanged} unchanged ·{" "}
              {report.counts.unavailable_metrics} unavailable
            </p>
            <p>
              {report.counts.rows} total groups: {report.counts.compared}{" "}
              compared, {report.counts.incomplete} incomplete,{" "}
              {report.counts.incomparable} incompatible, {report.counts.errors}{" "}
              errors, {report.counts.added} added, {report.counts.removed}{" "}
              removed.
            </p>
            <p className="muted">
              Delta means candidate minus baseline. An improved score does not
              erase a collision or a failed test.
            </p>
            <div
              className="table-scroll"
              role="region"
              tabIndex={0}
              aria-label="Comparison table"
            >
              <table>
                <caption>
                  Selected outcomes and metric changes. Counts above cover every
                  page.
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Baseline test</th>
                    <th scope="col">Candidate test</th>
                    <th scope="col">Review</th>
                    <th scope="col">Scores</th>
                  </tr>
                </thead>
                <tbody>
                  {report.rows.map((row) => (
                    <tr key={row.id}>
                      <td>
                        <Entries
                          entries={row.baseline}
                          requestID={baseline}
                          label="Baseline"
                          analysis={baselineAnalysis}
                          context={context}
                        />
                      </td>
                      <td>
                        <Entries
                          entries={row.candidate}
                          requestID={candidate}
                          label="Candidate"
                          analysis={candidateAnalysis}
                          context={context}
                        />
                      </td>
                      <td>
                        <State value={row.kind} />
                        {row.status_changed && <p>Overall status changed</p>}
                        {row.reason && (
                          <p className="muted">
                            {row.reason.replaceAll("_", " ").toLowerCase()}
                          </p>
                        )}
                      </td>
                      <td>
                        {row.metrics.length === 0 ? (
                          "No comparable scores"
                        ) : (
                          <ul className="metrics">
                            {row.metrics.map((metric) => (
                              <li key={metric.name}>
                                <strong>
                                  {metric.name.replaceAll("_", " ")}
                                </strong>{" "}
                                · {metric.unit}, v{metric.version}
                                <br />
                                {metric.baseline ?? "Unavailable"} →{" "}
                                {metric.candidate ?? "Unavailable"} · Δ{" "}
                                {metric.delta === null
                                  ? "Unavailable"
                                  : metric.delta > 0
                                    ? `+${metric.delta}`
                                    : metric.delta}
                                <br />
                                <State value={metric.change} />
                              </li>
                            ))}
                          </ul>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <nav className="actions" aria-label="Comparison pages">
              {after && (
                <a href={link({ ...context, view: "compare", after: "" })}>
                  First page
                </a>
              )}
              {data.next_after && (
                <a
                  href={link({
                    ...context,
                    view: "compare",
                    after: data.next_after,
                  })}
                >
                  Next page
                </a>
              )}
            </nav>
          </section>
        );
      }}
    </Load>
  );
}
