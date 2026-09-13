import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import {
  budgetsSchema,
  catalogSchema,
  createdSchema,
  fetchJSON,
  useLive,
} from "./api";
import { Load } from "./components";
import { navigate } from "./navigation";

const draftSchema = z.object({
  team: z.string().max(64).default(""),
  name: z.string().max(64),
  collection: z.string().max(64),
  controller: z.string().max(64),
  suites: z.array(z.string().max(64)).max(1000),
  seed: z.string().max(16),
});
type Draft = z.infer<typeof draftSchema>;
const storageKey = "copernicus-request-draft-v1";
function emptyDraft(): Draft {
  return {
    name: `review-${crypto.randomUUID()}`,
    team: "",
    collection: "",
    controller: "",
    suites: [],
    seed: "0",
  };
}
function loadDraft(): Draft {
  try {
    const value: unknown = JSON.parse(
      sessionStorage.getItem(storageKey) ?? "null",
    );
    return draftSchema.parse(value);
  } catch {
    return emptyDraft();
  }
}
export function CreateRequest() {
  const budgets = useLive("/api/budgets", budgetsSchema);
  const catalog = useLive("/api/catalog", catalogSchema);
  const client = useQueryClient();
  const [draft, setDraft] = useState(loadDraft);
  const [storageFailed, setStorageFailed] = useState(false);
  const [selectionError, setSelectionError] = useState(false);
  const mutation = useMutation({
    mutationFn: (input: Draft) =>
      fetchJSON("/api/requests", createdSchema, undefined, {
        ...(input.team ? { team_id: input.team } : {}),
        id: input.name,
        collection_id: input.collection,
        controller_id: input.controller,
        suite_ids: input.suites,
        seed: Number(input.seed),
        repeat: 0,
        priority: 1,
        requester: "reviewer",
      }),
    onSuccess: (record) => {
      void client.invalidateQueries({ queryKey: ["requests"] });
      void client.invalidateQueries({ queryKey: ["/api/budgets"] });
      update(emptyDraft());
      navigate({ view: "request", id: record.request_id });
    },
  });
  function update(value: Draft) {
    setDraft(value);
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(value));
      setStorageFailed(false);
    } catch {
      setStorageFailed(true);
    }
  }
  return (
    <>
      <h1>Create a test request</h1>
      <p className="lead">
        Choose test groups and a braking rule. Copernicus saves the exact inputs
        for later review.
      </p>
      <Load query={catalog} name="catalog">
        {(data) =>
          data.collections.length === 0 || data.controllers.length === 0 ? (
            <p className="notice">
              No tests are available. Import the example catalog, then retry.
              See the review guide supplied with this project.
            </p>
          ) : (
            <form
              onSubmit={(event) => {
                event.preventDefault();
                if (mutation.isPending) return;
                if (draft.suites.length === 0) {
                  setSelectionError(true);
                  document.getElementById("suites")?.focus();
                  return;
                }
                setSelectionError(false);
                mutation.mutate(draft);
              }}
            >
              <fieldset disabled={mutation.isPending} className="form-fields">
                <label>
                  Request name
                  <input
                    required
                    pattern="[a-z][a-z0-9_-]{0,63}"
                    maxLength={64}
                    value={draft.name}
                    onChange={(event) =>
                      update({ ...draft, name: event.target.value })
                    }
                    aria-describedby="name-help"
                  />
                </label>
                <p id="name-help" className="muted">
                  Use lowercase letters, numbers, underscores, or hyphens. Start
                  with a letter. Each name identifies one saved request.
                </p>
                <Load query={budgets} name="team budgets">
                  {(items) => (
                    <div>
                      <label>
                        Team budget
                        <select
                          value={draft.team || "local"}
                          onChange={(event) =>
                            update({
                              ...draft,
                              team:
                                event.target.value === "local"
                                  ? ""
                                  : event.target.value,
                            })
                          }
                        >
                          {!items.some(
                            (item) => item.team_id === (draft.team || "local"),
                          ) && (
                            <option value={draft.team || "local"} disabled>
                              Unavailable team: {draft.team || "local"}
                            </option>
                          )}
                          {items.map((item) => (
                            <option key={item.team_id} value={item.team_id}>
                              {item.team_id}
                            </option>
                          ))}
                        </select>
                      </label>
                      <p className="muted">
                        {items
                          .find(
                            (item) => item.team_id === (draft.team || "local"),
                          )
                          ?.remaining_ticks.toLocaleString() ??
                          "Unavailable"}{" "}
                        simulation ticks remain. Each request reserves its
                        maximum test length. Saved recordings need no additional
                        simulation ticks.
                      </p>
                    </div>
                  )}
                </Load>
                <label>
                  Test collection
                  <select
                    required
                    value={draft.collection}
                    onChange={(event) =>
                      update({
                        ...draft,
                        collection: event.target.value,
                        suites: [],
                      })
                    }
                  >
                    <option value="">Choose a collection</option>
                    {data.collections.map((item) => (
                      <option key={item.id}>{item.id}</option>
                    ))}
                  </select>
                </label>
                <fieldset
                  id="suites"
                  tabIndex={-1}
                  aria-describedby="suite-help suite-error"
                  aria-invalid={selectionError}
                >
                  <legend>Suites to run</legend>
                  <p id="suite-help" className="muted">
                    Select one or more groups. Shared tests run once.
                  </p>
                  {data.collections
                    .find((item) => item.id === draft.collection)
                    ?.suite_ids.map((suiteID) => {
                      const suite = data.suites.find(
                        (item) => item.id === suiteID,
                      );
                      return (
                        <label className="check" key={suiteID}>
                          <input
                            type="checkbox"
                            checked={draft.suites.includes(suiteID)}
                            onChange={(event) =>
                              update({
                                ...draft,
                                suites: event.target.checked
                                  ? [...draft.suites, suiteID]
                                  : draft.suites.filter(
                                      (value) => value !== suiteID,
                                    ),
                              })
                            }
                          />
                          <span>
                            {suiteID}{" "}
                            <small>{suite?.test_ids.length ?? 0} tests</small>
                          </span>
                        </label>
                      );
                    }) ?? <p>Choose a collection to see its suites.</p>}
                  <p
                    id="suite-error"
                    role={selectionError ? "alert" : undefined}
                  >
                    {selectionError ? "Select at least one suite." : ""}
                  </p>
                </fieldset>
                <label>
                  Braking rule
                  <select
                    required
                    value={draft.controller}
                    onChange={(event) =>
                      update({ ...draft, controller: event.target.value })
                    }
                  >
                    <option value="">Choose a rule</option>
                    {data.controllers.map((item) => (
                      <option value={item.id} key={item.id}>
                        {item.id} · version {item.version}
                      </option>
                    ))}
                  </select>
                </label>
                <details>
                  <summary>Repeatable input settings</summary>
                  <label>
                    Random seed
                    <input
                      required
                      type="number"
                      min="0"
                      max="9007199254740991"
                      step="1"
                      value={draft.seed}
                      onChange={(event) =>
                        update({ ...draft, seed: event.target.value })
                      }
                    />
                  </label>
                  <p className="muted">
                    Use the same seed in both requests. The repeat number is
                    zero.
                  </p>
                </details>
                <p>
                  The request enters the local queue. The configured
                  command-line worker and result importer must process it before
                  results appear.
                </p>
                {mutation.isError && (
                  <p role="alert" className="notice">
                    {mutation.error.message} Your entries are still available.
                  </p>
                )}
                <button className="primary" type="submit">
                  {mutation.isPending ? "Saving request…" : "Create request"}
                </button>
              </fieldset>
              <p role="status" className="muted">
                {mutation.isPending
                  ? "Saving your request. Keep this page open."
                  : storageFailed
                    ? "Draft is held on this page only. Browser storage is unavailable."
                    : "Draft saved in this browser tab."}
              </p>
            </form>
          )
        }
      </Load>
    </>
  );
}
