import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  analysesSchema,
  analysisSchema,
  catalogSchema,
  fetchJSON,
  useLive,
} from "./api";
import { Load } from "./components";
import { navigate } from "./navigation";

export function AnalysisChoice({
  requestID,
  label,
  value,
  onChange,
}: {
  requestID: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const query = useLive(
    `/api/requests/${encodeURIComponent(requestID)}/analyses`,
    analysesSchema,
  );
  return (
    <Load query={query} name={`${label.toLowerCase()} choices`}>
      {(items) => (
        <label>
          {label}
          <select
            required
            value={value}
            onChange={(event) => onChange(event.target.value)}
          >
            {!items.some((item) => item.id === value) && (
              <option value={value} disabled>
                Unavailable selection: {value}
              </option>
            )}
            {items.map((item) => (
              <option key={item.id} value={item.id}>
                {item.id === "original" ? "Original scores" : item.id}
              </option>
            ))}
          </select>
        </label>
      )}
    </Load>
  );
}

export function NewAnalysis({ requestID }: { requestID: string }) {
  const [template, setTemplate] = useState("");
  const catalog = useLive("/api/catalog", catalogSchema);
  const client = useQueryClient();
  const endpoint = `/api/requests/${encodeURIComponent(requestID)}/analyses`;
  const mutation = useMutation({
    mutationFn: () =>
      fetchJSON(endpoint, analysisSchema, undefined, { template_id: template }),
    onSuccess: (saved) => {
      void client.invalidateQueries({ queryKey: [endpoint] });
      navigate({ view: "request", id: requestID, analysis: saved.id });
    },
  });
  return (
    <details>
      <summary>Score these recordings again</summary>
      <p>
        Choose scoring settings for every test in this request. Original scores
        and recordings stay available. Simulation does not run again.
      </p>
      <p>
        Every test needs an imported original result and its recording. New
        scores appear after workers process the queued analysis jobs.
      </p>
      <Load query={catalog} name="scoring settings">
        {(data) => (
          <form
            onSubmit={(event) => {
              event.preventDefault();
              if (!mutation.isPending) mutation.mutate();
            }}
          >
            <fieldset disabled={mutation.isPending}>
              <label>
                New scoring settings
                <select
                  required
                  value={template}
                  onChange={(event) => setTemplate(event.target.value)}
                >
                  <option value="">Choose scoring settings</option>
                  {data.analysis_templates
                    .filter((item) => item.id !== "original")
                    .map((item) => (
                      <option key={item.id}>{item.id}</option>
                    ))}
                </select>
              </label>
              <p>
                Each named setting is fixed. Repeating a saved selection opens
                the same scores.
              </p>
              {mutation.isError && (
                <p role="alert" className="notice">
                  {mutation.error.message} Your selection is still available.
                </p>
              )}
              <button type="submit" className="primary">
                {mutation.isPending
                  ? "Saving score request…"
                  : "Request new scores"}
              </button>
            </fieldset>
            <p role="status">
              {mutation.isPending
                ? "Saving your selection. Keep this page open."
                : ""}
            </p>
          </form>
        )}
      </Load>
    </details>
  );
}
