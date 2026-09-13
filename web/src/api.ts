import { z } from "zod";
import { useQuery } from "@tanstack/react-query";

export const id = z.string().regex(/^[a-z][a-z0-9_-]{0,63}$/);
const hash = z.string().regex(/^[a-f0-9]{64}$/);
const count = z.number().int().nonnegative();
const integer = z.number().int().safe();
const text = z.string().max(256);
const frozen = z.object({
  id,
  sha256: hash,
  content: z.record(z.string(), z.json()),
});
export const catalogSchema = z.object({
  analysis_templates: z.array(z.object({ id })),
  controllers: z.array(z.object({ id, name: id, version: count })),
  suites: z.array(z.object({ id, test_ids: z.array(id) })),
  collections: z.array(z.object({ id, suite_ids: z.array(id) })),
});
const resultReference = z.object({
  path: text,
  sha256: hash,
  analysis_id: text.nullable(),
});
const state = z.enum([
  "QUEUED",
  "PUBLISHED",
  "PENDING",
  "RUNNING",
  "ANALYZING",
  "INCOMPLETE",
  "PASS",
  "FAIL",
  "WARN",
  "ERROR",
  "RESOLUTION_FAILED",
]);
const executionProgress = z.object({
  execution_id: id,
  test_id: id,
  state,
  reason: text.optional(),
  result: resultReference.optional(),
});
const totals = z.object({
  total: count,
  completed: count,
  incomplete: count,
  passed: count,
  failed: count,
  warnings: count,
  errors: count,
  resolution_failed: count,
  complete: z.boolean(),
});
export const analysisSchema = z.object({ id, template: frozen.nullable() });
export const analysesSchema = z.array(analysisSchema);
export const progressSchema = totals.extend({
  analysis_selection: id,
  request_id: id,
  executions: z.array(executionProgress),
});
const selection = totals.extend({
  analysis_selection: id,
  request_id: id,
  snapshot_sha256: hash,
  controller_sha256: hash,
});
const entry = z.object({
  execution_id: id,
  test_id: id,
  state,
  reason: text.optional(),
  result: resultReference.nullable(),
});
const metric = z.object({
  name: text,
  unit: text,
  version: count,
  baseline: integer.nullable(),
  candidate: integer.nullable(),
  delta: integer.nullable(),
  change: z.enum(["REGRESSION", "IMPROVEMENT", "UNCHANGED", "UNAVAILABLE"]),
});
export const comparisonSchema = z.object({
  comparison: z.object({
    baseline: selection,
    candidate: selection,
    counts: z.object({
      rows: count,
      compared: count,
      incomparable: count,
      incomplete: count,
      errors: count,
      added: count,
      removed: count,
      regressions: count,
      improvements: count,
      unchanged: count,
      unavailable_metrics: count,
    }),
    rows: z.array(
      z.object({
        id: hash,
        kind: z.enum([
          "COMPARED",
          "INCOMPARABLE",
          "INCOMPLETE",
          "ERROR",
          "ADDED",
          "REMOVED",
        ]),
        reason: text.optional(),
        baseline: z.array(entry),
        candidate: z.array(entry),
        metrics: z.array(metric),
        status_changed: z.boolean().nullable(),
      }),
    ),
  }),
  next_after: hash.optional(),
});
export const reviewSchema = z.object({
  analysis_selection: id,
  selected_analysis: frozen,
  request_id: id,
  snapshot_sha256: hash,
  controller: frozen,
  source: frozen,
  execution: z.object({
    id,
    test: frozen,
    scenario: frozen,
    run_template: frozen,
    analysis_template: frozen,
    simulator: frozen,
    seed: integer,
    repeat: count,
  }),
  progress: executionProgress,
  outcome: z
    .object({
      status: state,
      inputs_hash: hash,
      bag: z.object({ path: text, sha256: hash }).nullable(),
      metrics: z
        .record(
          z.string(),
          z.object({
            value: integer.nullable(),
            unit: text,
            version: count,
            pass: z.boolean().nullable(),
            evidence_ticks: z.array(count),
          }),
        )
        .nullable(),
    })
    .nullable(),
});
export const tickSchema = z.object({
  tick: count,
  time_ms: integer,
  position_mm: integer,
  speed_mm_s: integer,
  acceleration_mm_s2: integer,
  obstacles: z.array(
    z.object({ position_mm: integer, speed_mm_s: integer, length_mm: integer }),
  ),
});
export const createdSchema = z.object({
  request_id: id,
  snapshot_sha256: hash,
});
const requestsSchema = z.object({
  request_ids: z.array(id),
  next_after: id.optional(),
});

export async function fetchJSON<T>(
  path: string,
  schema: z.ZodType<T>,
  signal?: AbortSignal,
  body?: unknown,
): Promise<T> {
  const response = await fetch(path, {
    signal: signal ?? null,
    ...(body === undefined
      ? {}
      : {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Copernicus-Request": "1",
          },
          body: JSON.stringify(body),
        }),
  });
  if (!response.ok) {
    const messages: Record<number, string> = {
      400: path.endsWith("/analyses")
        ? "New scores need supported scoring settings and an original recording for every test. Import missing results, then retry."
        : "Check your request name and selected suites. Reload the catalog if its tests changed.",
      403: "Access was denied. Open the review server at its printed local address.",
      404: "This item is unavailable. Return to the request list or retry after importing results.",
      409: "This request name already has different inputs. Choose a new name, or open the saved request.",
      429: "The local server is busy. Wait a moment, then retry.",
    };
    throw new Error(
      messages[response.status] ??
        "The local server could not complete this read. Check that it is running and retry.",
    );
  }
  const value: unknown = await response.json();
  const parsed = schema.safeParse(value);
  if (!parsed.success)
    throw new Error(
      "The server returned unexpected data. Check the installed version and retry.",
    );
  return parsed.data;
}

export function useLive<T>(path: string, schema: z.ZodType<T>) {
  return useQuery({
    queryKey: [path],
    queryFn: ({ signal }) => fetchJSON(path, schema, signal),
    refetchInterval: 5000,
    retry: false,
    gcTime: 0,
  });
}

export function useRequests() {
  return useQuery({
    queryKey: ["requests"],
    retry: false,
    refetchInterval: 5000,
    queryFn: async ({ signal }) => {
      const ids: string[] = [];
      let after = "";
      for (let page = 0; page < 10; page++) {
        const result = await fetchJSON(
          `/api/requests?limit=100&after=${encodeURIComponent(after)}`,
          requestsSchema,
          signal,
        );
        ids.push(...result.request_ids);
        if (!result.next_after) return ids;
        if (result.next_after <= after)
          throw new Error("Request listing could not advance. Retry the read.");
        after = result.next_after;
      }
      throw new Error("The request listing exceeds the supported size.");
    },
  });
}
export type Catalog = z.infer<typeof catalogSchema>;
export type Progress = z.infer<typeof progressSchema>;
export type Entry = z.infer<typeof entry>;
export type Frozen = z.infer<typeof frozen>;
