import { progressSchema, useLive, useRequests } from "./api";
import { Load, State, Totals } from "./components";
import { AnalysisChoice, NewAnalysis } from "./Analysis";
import { link, navigate } from "./navigation";

export function Requests() {
  const query = useRequests();
  return (
    <>
      <div className="title-row">
        <div>
          <p className="eyebrow">Local test history</p>
          <h1>Review your requests</h1>
        </div>
        <a className="primary" href={link({ view: "create" })}>
          Create request
        </a>
      </div>
      <p className="lead">
        Open a request to follow its progress, or compare two requests to find
        what changed.
      </p>
      <Load query={query} name="requests">
        {(ids) =>
          ids.length === 0 ? (
            <div className="notice">
              <h2>No requests yet</h2>
              <p>
                Create your first request with the example suites. Its saved
                inputs will remain available for review.
              </p>
            </div>
          ) : (
            <ul className="request-list">
              {ids.map((id) => (
                <li key={id}>
                  <a href={link({ view: "request", id })}>
                    {id}
                    <span aria-hidden="true"> →</span>
                  </a>
                </li>
              ))}
            </ul>
          )
        }
      </Load>
    </>
  );
}
export function RequestView({
  id,
  analysis,
}: {
  id: string;
  analysis: string;
}) {
  const query = useLive(
    `/api/requests/${encodeURIComponent(id)}/status?analysis=${encodeURIComponent(analysis)}`,
    progressSchema,
  );
  return (
    <>
      <p className="eyebrow">Saved request</p>
      <h1>{id}</h1>
      <AnalysisChoice
        requestID={id}
        label="Scores to review"
        value={analysis}
        onChange={(value) => navigate({ view: "request", id, analysis: value })}
      />
      <NewAnalysis requestID={id} />
      <Load query={query} name="progress">
        {(data) => (
          <>
            <Totals data={data} />
            <p className="actions">
              <a
                className="primary"
                href={link({
                  view: "compare",
                  baseline: id,
                  baseline_analysis: analysis,
                })}
              >
                Compare this request
              </a>
              <a href={link({ view: "create" })}>Create another request</a>
            </p>
            {!data.complete && (
              <details>
                <summary>Waiting for results?</summary>
                <p>
                  Publish queued jobs, run Yamata workers, and import their
                  results. This page updates automatically after import.
                </p>
                <p>
                  Follow the command sequence in the project’s review guide.
                  Input failures require a new request with supported tests.
                </p>
              </details>
            )}
            <div
              className="table-scroll"
              tabIndex={0}
              role="region"
              aria-label="Test progress"
            >
              <table>
                <caption>
                  Every selected test, including incomplete tests
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Test</th>
                    <th scope="col">State</th>
                    <th scope="col">Review</th>
                  </tr>
                </thead>
                <tbody>
                  {data.executions.map((item) => (
                    <tr key={item.execution_id}>
                      <th scope="row">{item.test_id}</th>
                      <td>
                        <State value={item.state} />
                        {item.reason && (
                          <p className="muted">
                            {item.reason.replaceAll("_", " ").toLowerCase()}
                          </p>
                        )}
                      </td>
                      <td>
                        <a
                          href={link({
                            view: "execution",
                            id,
                            execution: item.execution_id,
                            analysis,
                          })}
                        >
                          Inputs and evidence for {item.test_id}
                        </a>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </Load>
    </>
  );
}
