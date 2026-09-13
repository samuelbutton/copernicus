import { reviewSchema, tickSchema, useLive } from "./api";
import { Inputs, Load, State } from "./components";
import { link } from "./navigation";
export function Execution({ route }: { route: URLSearchParams }) {
  const id = route.get("id") ?? "",
    execution = route.get("execution") ?? "",
    analysis = route.get("analysis") || "original";
  const endpoint = `/api/requests/${encodeURIComponent(id)}/executions/${encodeURIComponent(execution)}`;
  const query = useLive(
    `${endpoint}?analysis=${encodeURIComponent(analysis)}`,
    reviewSchema,
  );
  const context = Object.fromEntries(route),
    tick = route.get("tick"),
    baseline = route.get("baseline") ?? "",
    candidate = route.get("candidate") ?? "";
  return (
    <>
      <p className="eyebrow">Request: {id}</p>
      <h1>Inputs and evidence</h1>
      <p className="actions">
        <a href={link({ view: "request", id, analysis })}>Back to request</a>
        {baseline && candidate && (
          <a
            href={link({
              view: "compare",
              baseline,
              candidate,
              after: route.get("after") ?? "",
              filter: route.get("filter") || "all",
              sort: route.get("sort") || "id",
              limit: route.get("limit") || "25",
              baseline_analysis: route.get("baseline_analysis") || "original",
              candidate_analysis: route.get("candidate_analysis") || "original",
            })}
          >
            Back to comparison
          </a>
        )}
      </p>
      <Load query={query} name="test evidence">
        {(data) => (
          <>
            <h2>
              {data.execution.test.id} <State value={data.progress.state} />
            </h2>
            <p>
              These inputs were saved when the request was created. Later
              catalog edits cannot change them.
            </p>
            {data.outcome?.metrics ? (
              <div
                className="table-scroll"
                tabIndex={0}
                role="region"
                aria-label="Metric evidence"
              >
                <table>
                  <caption>
                    Scores and the recording ticks that support them
                  </caption>
                  <thead>
                    <tr>
                      <th scope="col">Metric</th>
                      <th scope="col">Value</th>
                      <th scope="col">Outcome</th>
                      <th scope="col">Evidence ticks</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(data.outcome.metrics).map(
                      ([name, metric]) => (
                        <tr key={name}>
                          <th scope="row">
                            {name.replaceAll("_", " ")}
                            <br />
                            <small>Version {metric.version}</small>
                          </th>
                          <td>
                            {metric.value ?? "Unavailable"} {metric.unit}
                          </td>
                          <td>
                            {metric.pass === null
                              ? "Unavailable"
                              : metric.pass
                                ? "Passed"
                                : "Failed"}
                          </td>
                          <td>
                            {metric.evidence_ticks.length ? (
                              <div className="tick-links">
                                {metric.evidence_ticks.map((value) => (
                                  <a
                                    aria-current={
                                      tick === String(value)
                                        ? "true"
                                        : undefined
                                    }
                                    key={value}
                                    href={link({
                                      ...context,
                                      tick: String(value),
                                    })}
                                  >
                                    Tick {value}
                                  </a>
                                ))}
                              </div>
                            ) : (
                              "No cited ticks"
                            )}
                          </td>
                        </tr>
                      ),
                    )}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="notice">
                No measured scores are available. Check progress and retry after
                results are imported.
              </p>
            )}
            {tick !== null && (
              <Evidence endpoint={endpoint} tick={tick} analysis={analysis} />
            )}
            <h2>Selected scores: {data.analysis_selection}</h2>
            <Inputs
              name="Selected score limits"
              value={data.selected_analysis}
            />
            <h2>Saved inputs</h2>
            <p>
              Seed: {data.execution.seed} · Repeat: {data.execution.repeat}
            </p>
            <Inputs name="Scenario" value={data.execution.scenario} />
            <Inputs name="Braking rule" value={data.controller} />
            <Inputs name="Run settings" value={data.execution.run_template} />
            <Inputs
              name="Original score limits"
              value={data.execution.analysis_template}
            />
            <Inputs name="Test definition" value={data.execution.test} />
            <Inputs name="Simulator" value={data.execution.simulator} />
            <Inputs name="Execution source" value={data.source} />
            <details>
              <summary>Result identity</summary>
              <p className="hash">Snapshot SHA256: {data.snapshot_sha256}</p>
              <pre>{JSON.stringify(data.progress.result ?? null, null, 2)}</pre>
              {data.outcome?.bag && (
                <pre>{JSON.stringify(data.outcome.bag, null, 2)}</pre>
              )}
            </details>
          </>
        )}
      </Load>
    </>
  );
}
function Evidence({
  endpoint,
  tick,
  analysis,
}: {
  endpoint: string;
  tick: string;
  analysis: string;
}) {
  const query = useLive(
    `${endpoint}/ticks/${encodeURIComponent(tick)}?analysis=${encodeURIComponent(analysis)}`,
    tickSchema,
  );
  return (
    <section aria-label={`Evidence tick ${tick}`}>
      <h2>Evidence tick {tick}</h2>
      <Load query={query} name="recording tick">
        {(data) => (
          <>
            <p>Time in recording: {data.time_ms} milliseconds.</p>
            <dl className="tick-facts">
              <div>
                <dt>Vehicle position</dt>
                <dd>{data.position_mm} mm</dd>
              </div>
              <div>
                <dt>Vehicle speed</dt>
                <dd>{data.speed_mm_s} mm/s</dd>
              </div>
              <div>
                <dt>Acceleration</dt>
                <dd>{data.acceleration_mm_s2} mm/s²</dd>
              </div>
            </dl>
            {data.obstacles.length > 0 ? (
              <div
                className="table-scroll"
                role="region"
                tabIndex={0}
                aria-label="Obstacles at this tick"
              >
                <table>
                  <caption>Obstacles at tick {data.tick}</caption>
                  <thead>
                    <tr>
                      <th scope="col">Obstacle</th>
                      <th scope="col">Position (mm)</th>
                      <th scope="col">Speed (mm/s)</th>
                      <th scope="col">Length (mm)</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.obstacles.map((obstacle, index) => (
                      <tr key={index}>
                        <th scope="row">{index + 1}</th>
                        <td>{obstacle.position_mm}</td>
                        <td>{obstacle.speed_mm_s}</td>
                        <td>{obstacle.length_mm}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p>No obstacles in this recording tick.</p>
            )}
            <details>
              <summary>Recording fields</summary>
              <pre>{JSON.stringify(data, null, 2)}</pre>
            </details>
          </>
        )}
      </Load>
    </section>
  );
}
