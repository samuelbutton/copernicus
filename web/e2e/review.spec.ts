import { test as base, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import {
  mkdtemp,
  rm,
  mkdir,
  readdir,
  readFile,
  rename,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { z } from "zod";

const run = promisify(execFile);
const root = resolve(import.meta.dirname, "../..");
const cli = join(root, "bin/copernicus");
type App = {
  url: string;
  directory: string;
  db: string;
  exchange: string;
  processJobs: (analysisOnly?: boolean) => Promise<void>;
};
const test = base.extend<{ app: App }>({
  app: async ({ request }, provide) => {
    const directory = await mkdtemp(join(tmpdir(), "copernicus-browser-")),
      db = join(directory, "catalog.sqlite"),
      exchange = join(directory, "exchange");
    await mkdir(exchange);
    await run(cli, [
      "catalog",
      "import",
      "--db",
      db,
      "--file",
      join(root, "examples/catalog.json"),
    ]);
    const server = spawn(
      cli,
      [
        "serve",
        "--db",
        db,
        "--web-dir",
        join(root, "web/dist"),
        "--duckdb",
        join(root, "bin/duckdb"),
        "--port",
        "0",
      ],
      { cwd: root, stdio: ["ignore", "pipe", "pipe"] },
    );
    try {
      const url = await new Promise<string>((resolve, reject) => {
        const timeout = setTimeout(
          () => reject(new Error("Review server did not start")),
          10000,
        );
        server.once("error", (err) => {
          clearTimeout(timeout);
          reject(err);
        });
        server.once("exit", () => {
          clearTimeout(timeout);
          reject(new Error("Review server exited"));
        });
        let output = "";
        server.stdout.on("data", (data: Buffer) => {
          output += data.toString();
          const match = /http:\/\/127\.0\.0\.1:\d+/.exec(output);
          if (match) {
            clearTimeout(timeout);
            resolve(match[0]);
          }
        });
      });
      expect((await request.get(url + "/readyz")).ok()).toBe(true);
      await provide({
        url,
        directory,
        db,
        exchange,
        processJobs: async (analysisOnly = false) => {
          const engine = process.env["YAMATA_BIN"];
          if (!engine)
            throw new Error(
              "Set YAMATA_BIN to the executable built from compatibility/yamata.json",
            );
          await run(cli, [
            "outbox",
            "publish",
            "--db",
            db,
            "--exchange-dir",
            exchange,
          ]);
          for (const job of await readdir(join(exchange, "jobs")))
            await run(engine, [
              "enqueue",
              "--exchange-dir",
              exchange,
              `jobs/${job}`,
            ]);
          await run(engine, [
            "workers",
            "--exchange-dir",
            exchange,
            "--drain",
            ...(analysisOnly
              ? ["--simulation-workers", "0", "--analysis-workers", "1"]
              : []),
          ]);
          await run(cli, [
            "results",
            "import",
            "--db",
            db,
            "--exchange-dir",
            exchange,
          ]);
        },
      });
    } finally {
      if (server.exitCode === null) {
        const ended = new Promise<void>((resolve) =>
          server.once("exit", () => resolve()),
        );
        server.kill("SIGTERM");
        await ended;
      }
      await rm(directory, { recursive: true, force: true });
    }
  },
});
async function accessible(page: Page) {
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(result.violations).toEqual([]);
}
async function create(
  page: Page,
  name: string,
  controller: string,
  suites = ["smoke"],
) {
  await page.goto(page.url().split("#")[0] + "#view=create");
  await page.getByLabel("Request name").fill(name);
  await page.getByLabel("Test collection").selectOption("all-tests");
  for (const suite of suites)
    await page.getByRole("checkbox", { name: new RegExp(`^${suite}`) }).check();
  await page.getByLabel("Braking rule").selectOption(controller);
  await page
    .getByRole("button", { name: "Create request", exact: true })
    .click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
}

test("suite selection, draft recovery, keyboard navigation, and partial review", async ({
  page,
  app,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(app.url);
  await expect(
    page.getByRole("heading", { name: "No requests yet" }),
  ).toBeVisible();
  await accessible(page);
  await page.keyboard.press("Tab");
  await expect(
    page.getByRole("link", { name: "Create request", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Enter");
  await page.getByLabel("Request name").fill("browser-baseline");
  await page.getByLabel("Test collection").selectOption("all-tests");
  await page.getByRole("checkbox", { name: /^smoke/ }).focus();
  await page.keyboard.press("Space");
  await expect(page.getByRole("checkbox", { name: /^smoke/ })).toBeChecked();
  await page.getByLabel("Braking rule").selectOption("baseline");
  await page.reload();
  await expect(page.getByLabel("Request name")).toHaveValue("browser-baseline");
  await expect(page.getByRole("checkbox", { name: /^smoke/ })).toBeChecked();
  await accessible(page);
  await page.route("**/api/requests", async (route) => {
    if (route.request().method() === "POST")
      await route.fulfill({ status: 503, json: { error: "unavailable" } });
    else await route.continue();
  });
  await page
    .getByRole("button", { name: "Create request", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Your entries are still available",
  );
  await expect(page.getByLabel("Request name")).toHaveValue("browser-baseline");
  await page.unroute("**/api/requests");
  await page
    .getByRole("button", { name: "Create request", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "browser-baseline", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("0 of 2", { exact: true })).toBeVisible();
  await expect(page.getByText(/Partial results: 2/)).toBeVisible();
  await accessible(page);
  await create(page, "browser-candidate", "candidate");
  await page.getByRole("link", { name: "Compare", exact: true }).click();
  await page.getByLabel("Baseline request").selectOption("browser-baseline");
  await page.getByLabel("Candidate request").selectOption("browser-candidate");
  await page.getByRole("button", { name: "Compare requests" }).click();
  await expect(
    page.getByText(/2 total groups: 0 compared, 2 incomplete/),
  ).toBeVisible();
  await accessible(page);
  await page
    .getByRole("link", { name: "Candidate: stopped-obstacle", exact: true })
    .click();
  await expect(page.getByText(/No measured scores/)).toBeVisible();
  await page.getByText("Scenario: stopped-obstacle", { exact: true }).click();
  await expect(page.locator("details[open]")).toContainText("goal_position_mm");
  await accessible(page);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Inputs and evidence" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Back to comparison" }).click();
  await expect(page.getByLabel("Baseline request")).toHaveValue(
    "browser-baseline",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await accessible(page);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  expect(errors).toEqual([]);
});

test("loading, malformed responses, denied reads, and recovery", async ({
  page,
  app,
}) => {
  let release = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/api/requests?*", async (route) => {
    await gate;
    await route.continue();
  });
  await page.goto(app.url);
  await expect(page.getByRole("status")).toContainText("Loading requests");
  release();
  await expect(
    page.getByRole("heading", { name: "No requests yet" }),
  ).toBeVisible();
  await page.unroute("**/api/requests?*");
  await page.route("**/api/requests?*", (route) =>
    route.fulfill({ json: { request_ids: ["<script>"] } }),
  );
  await page.reload();
  await expect(page.getByRole("alert")).toContainText("unexpected data");
  await page.unroute("**/api/requests?*");
  await page.route("**/api/requests?*", (route) =>
    route.fulfill({ status: 403, json: { error: "denied" } }),
  );
  await page.getByRole("button", { name: "Retry requests" }).click();
  await expect(page.getByRole("alert")).toContainText("Access was denied");
  await page.unroute("**/api/requests?*");
  await page.getByRole("button", { name: "Retry requests" }).click();
  await expect(
    page.getByRole("heading", { name: "No requests yet" }),
  ).toBeVisible();
  await accessible(page);
});

test("real baseline to candidate review and cited recording evidence", async ({
  page,
  app,
}, testInfo) => {
  // This acceptance test deliberately requires the public engine executable.
  await page.goto(app.url);
  await create(page, "baseline-review", "baseline", ["smoke", "obstacles"]);
  await create(page, "candidate-review", "candidate", ["smoke", "obstacles"]);
  await app.processJobs();
  await expect(page.getByText("3 of 3", { exact: true })).toBeVisible();
  await page.goto(
    app.url +
      "#view=compare&baseline=baseline-review&candidate=candidate-review",
  );
  await expect(page.getByText(/3 total groups: 3 compared/)).toBeVisible();
  const row = page.getByRole("row").filter({
    has: page.getByRole("link", {
      name: "Candidate: stopped-obstacle",
      exact: true,
    }),
  });
  await expect(row).toContainText("0 → 1 · Δ +1");
  await expect(row).toContainText("regression");
  await accessible(page);
  await page.screenshot({
    path: testInfo.outputPath("comparison.png"),
    fullPage: true,
  });
  await row
    .getByRole("link", { name: "Candidate: stopped-obstacle", exact: true })
    .click();
  const collision = page
    .getByRole("row")
    .filter({ has: page.getByRole("rowheader", { name: /collision count/ }) });
  await collision.getByRole("link", { name: /Tick/ }).first().click();
  await expect(page.getByText(/Time in recording:/)).toBeVisible();
  await accessible(page);
  await page.screenshot({
    path: testInfo.outputPath("evidence.png"),
    fullPage: true,
  });
  const files = await readdir(join(app.exchange, "results"));
  const schema = z.object({
    correlation_id: z.string(),
    bag: z.object({ path: z.string() }),
  });
  for (const file of files) {
    const value: unknown = JSON.parse(
      await readFile(join(app.exchange, "results", file), "utf8"),
    );
    const result = schema.parse(value);
    if (result.correlation_id === "candidate-review")
      await rename(
        join(app.exchange, result.bag.path),
        join(app.directory, file + ".removed"),
      );
  }
  await expect(page.getByText(/No measured scores/)).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("unavailable");
  await page.getByRole("link", { name: "Back to comparison" }).click();
  await expect(
    page.getByText(/3 total groups: 0 compared, 3 incomplete/),
  ).toBeVisible();
  await expect(
    page.getByText("0 metric regressions", { exact: true }),
  ).toBeVisible();
});

async function savedArtifacts(exchange: string) {
  const files = new Map<string, Buffer>();
  for (const directory of ["bags", "results", "events"])
    for (const name of await readdir(join(exchange, directory))) {
      const path = `${directory}/${name}`;
      files.set(path, await readFile(join(exchange, path)));
    }
  return files;
}
async function newScores(page: Page, app: App, id: string) {
  await page.goto(`${app.url}#view=request&id=${id}`);
  if (!(await page.getByLabel("New scoring settings").isVisible()))
    await page
      .getByText("Score these recordings again", { exact: true })
      .click();
  await page.getByLabel("New scoring settings").selectOption("edge-v2");
  await page
    .getByRole("button", { name: "Request new scores", exact: true })
    .click();
  await expect(page.getByLabel("Scores to review")).toHaveValue("edge-v2");
}

test("new scoring versions preserve recordings and restore explicit comparisons", async ({
  page,
  app,
}, testInfo) => {
  test.setTimeout(120000);
  await run(cli, [
    "catalog",
    "import",
    "--db",
    app.db,
    "--file",
    join(root, "examples/reanalysis-catalog.json"),
  ]);
  await page.goto(app.url);
  await create(page, "scores-baseline", "baseline", ["smoke", "obstacles"]);
  await create(page, "scores-candidate", "candidate", ["smoke", "obstacles"]);
  // Incomplete requests cannot silently omit tests from a scoring selection.
  await page.getByText("Score these recordings again", { exact: true }).click();
  await page.getByLabel("New scoring settings").selectOption("edge-v2");
  await page
    .getByRole("button", { name: "Request new scores", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "original recording for every test",
  );
  await app.processJobs();
  const engine = process.env["YAMATA_BIN"];
  if (!engine) throw new Error("Expected the pinned engine");
  const queueSchema = z.object({
    dispatch_positions: z.object({ simulation: z.number() }),
    jobs: z.array(
      z.object({ job_kind: z.string(), simulation_retries: z.number() }),
    ),
  });
  const queueBefore = queueSchema.parse(
    JSON.parse(
      (await run(engine, ["queue", "--exchange-dir", app.exchange])).stdout,
    ) as unknown,
  );
  const before = await savedArtifacts(app.exchange);
  const original = await run(cli, [
    "request",
    "status",
    "--db",
    app.db,
    "--id",
    "scores-candidate",
  ]);
  await newScores(page, app, "scores-candidate");
  await expect(page.getByText("0 of 3", { exact: true })).toBeVisible();
  await app.processJobs(true);
  await expect(page.getByText("3 of 3", { exact: true })).toBeVisible();
  await page.getByLabel("Scores to review").selectOption("original");
  await expect(page.getByText("3 of 3", { exact: true })).toBeVisible();
  await page.goto(
    `${app.url}#view=compare&baseline=scores-baseline&candidate=scores-candidate&baseline_analysis=original&candidate_analysis=edge-v2`,
  );
  await expect(
    page.getByText(/3 total groups: 0 compared, 0 incomplete, 3 incompatible/),
  ).toBeVisible();
  await expect(page.getByLabel("Baseline scores")).toHaveValue("original");
  await expect(page.getByLabel("Candidate scores")).toHaveValue("edge-v2");
  await accessible(page);
  await newScores(page, app, "scores-baseline");
  await app.processJobs(true);
  await page.goto(
    `${app.url}#view=compare&baseline=scores-baseline&candidate=scores-candidate&baseline_analysis=edge-v2&candidate_analysis=edge-v2`,
  );
  await expect(page.getByText(/3 total groups: 3 compared/)).toBeVisible();
  await accessible(page);
  await page.screenshot({
    path: testInfo.outputPath("matched-scores.png"),
    fullPage: true,
  });
  await page
    .getByRole("link", { name: "Candidate: stopped-obstacle", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Selected scores: edge-v2" }),
  ).toBeVisible();
  const gap = page.getByRole("row").filter({
    has: page.getByRole("rowheader", { name: /minimum obstacle gap/ }),
  });
  await expect(gap).toContainText("Version 2");
  await expect(
    gap.getByRole("cell", { name: "0 mm", exact: true }),
  ).toBeVisible();
  const collision = page
    .getByRole("row")
    .filter({ has: page.getByRole("rowheader", { name: /collision count/ }) });
  await collision.getByRole("link", { name: "Tick 22", exact: true }).click();
  await expect(
    page.getByText("Time in recording: 2200 milliseconds."),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Selected scores: edge-v2" }),
  ).toBeVisible();
  await accessible(page);
  await page.getByRole("link", { name: "Back to comparison" }).click();
  await expect(page.getByLabel("Baseline scores")).toHaveValue("edge-v2");
  await expect(page.getByLabel("Candidate scores")).toHaveValue("edge-v2");
  await page.getByLabel("Baseline scores").selectOption("original");
  await page.getByLabel("Candidate scores").selectOption("original");
  await page.getByRole("button", { name: "Compare requests" }).click();
  await expect(page.getByText(/3 total groups: 3 compared/)).toBeVisible();
  await page
    .getByRole("link", { name: "Candidate: stopped-obstacle", exact: true })
    .click();
  await expect(gap).toContainText("Version 1");
  await expect(gap).toContainText("3400 mm");
  // Same request on both sides must still respect distinct scoring selections.
  await page.goto(
    `${app.url}#view=compare&baseline=scores-candidate&candidate=scores-candidate&baseline_analysis=original&candidate_analysis=edge-v2`,
  );
  await expect(
    page.getByText(/3 total groups: 0 compared, 0 incomplete, 3 incompatible/),
  ).toBeVisible();
  const after = await savedArtifacts(app.exchange);
  const queueAfter = queueSchema.parse(
    JSON.parse(
      (await run(engine, ["queue", "--exchange-dir", app.exchange])).stdout,
    ) as unknown,
  );
  expect(queueBefore.dispatch_positions.simulation).toBe(6);
  expect(queueAfter.dispatch_positions.simulation).toBe(6);
  expect(
    queueAfter.jobs.filter((job) => job.job_kind === "analysis"),
  ).toHaveLength(6);
  expect(queueAfter.jobs.every((job) => job.simulation_retries === 0)).toBe(
    true,
  );

  for (const [path, bytes] of before)
    expect(after.get(path), path).toEqual(bytes);
  const recordingCount = (files: Map<string, Buffer>) =>
    [...files.keys()].filter((path) => path.startsWith("bags/")).length;
  expect(recordingCount(before)).toBe(6);
  expect(recordingCount(after)).toBe(6);
  const events = z.object({ state: z.string() });
  const simulations = (files: Map<string, Buffer>) =>
    [...files].filter(
      ([path, bytes]) =>
        path.startsWith("events/") &&
        events.parse(JSON.parse(bytes.toString()) as unknown).state ===
          "RUNNING",
    ).length;
  expect(simulations(before)).toBe(6);
  expect(simulations(after)).toBe(6);
  const outcome = z.object({
    execution_id: z.string(),
    analysis_id: z.string(),
    bag: z.object({ path: z.string(), sha256: z.string() }),
    metrics: z.object({
      minimum_obstacle_gap: z.object({ version: z.number() }),
    }),
  });
  const results = [...after]
    .filter(([path]) => path.startsWith("results/"))
    .map(([, bytes]) => outcome.parse(JSON.parse(bytes.toString()) as unknown));
  expect(results).toHaveLength(12);
  for (const first of results.filter(
    (r) => r.metrics.minimum_obstacle_gap.version === 1,
  )) {
    const second = results.find(
      (r) =>
        r.execution_id === first.execution_id &&
        r.metrics.minimum_obstacle_gap.version === 2,
    );
    expect(second?.analysis_id).not.toBe(first.analysis_id);
    expect(second?.bag).toEqual(first.bag);
  }
  const current = await run(cli, [
    "request",
    "status",
    "--db",
    app.db,
    "--id",
    "scores-candidate",
  ]);
  expect(current.stdout).toBe(original.stdout);
  // Retrying accepted work and rebuilding the derived index preserve both selections.
  await newScores(page, app, "scores-candidate");
  await app.processJobs(true);
  expect(await savedArtifacts(app.exchange)).toEqual(after);
  await run(cli, [
    "results",
    "rebuild",
    "--db",
    app.db,
    "--exchange-dir",
    app.exchange,
  ]);
  await page.reload();
  await expect(page.getByText("3 of 3", { exact: true })).toBeVisible();
  const originalAfterRebuild = await run(cli, [
    "request",
    "status",
    "--db",
    app.db,
    "--id",
    "scores-candidate",
  ]);
  expect(originalAfterRebuild.stdout).toBe(original.stdout);
  // Missing recordings never fall back to old scores or trigger simulation.
  const bag = results[0]?.bag.path;
  if (!bag) throw new Error("Expected a recording");
  await rename(join(app.exchange, bag), join(app.directory, "removed-bag"));
  await page.goto(
    `${app.url}#view=compare&baseline=scores-baseline&candidate=scores-candidate&baseline_analysis=edge-v2&candidate_analysis=edge-v2`,
  );
  await expect(
    page.getByText(/3 total groups: 2 compared, 1 incomplete/),
  ).toBeVisible();
});

test("team admission and saved comparison views remain repeat-safe", async ({
  page,
  app,
}, testInfo) => {
  await run(cli, [
    "budget",
    "set",
    "--db",
    app.db,
    "--team",
    "small",
    "--ticks",
    "199",
  ]);
  await page.goto(`${app.url}#view=create`);
  await page.getByLabel("Request name").fill("limited-review");
  await page.getByLabel("Team budget").selectOption("small");
  await page.getByLabel("Test collection").selectOption("all-tests");
  await page.getByRole("checkbox", { name: /^smoke/ }).check();
  await page.getByLabel("Braking rule").selectOption("baseline");
  await page
    .getByRole("button", { name: "Create request", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "exceeds the team’s remaining simulation ticks",
  );
  await expect(page.getByLabel("Request name")).toHaveValue("limited-review");
  const empty = JSON.parse(
    (await run(cli, ["outbox", "show", "--db", app.db])).stdout,
  ) as unknown;
  expect(JSON.stringify(empty)).not.toContain("limited-review");
  await accessible(page);
  await run(cli, [
    "budget",
    "set",
    "--db",
    app.db,
    "--team",
    "small",
    "--ticks",
    "200",
  ]);
  await page
    .getByRole("button", { name: "Create request", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "limited-review", exact: true }),
  ).toBeVisible();
  const retry = await page.request.post(`${app.url}/api/requests`, {
    headers: { Origin: app.url, "X-Copernicus-Request": "1" },
    data: {
      id: "limited-review",
      team_id: "small",
      collection_id: "all-tests",
      controller_id: "baseline",
      suite_ids: ["smoke"],
      seed: 0,
      repeat: 0,
      priority: 1,
      requester: "reviewer",
    },
  });
  expect(retry.ok()).toBe(true);
  const budgetSchema = z.array(
    z.object({
      team_id: z.string(),
      reserved_ticks: z.number(),
      remaining_ticks: z.number(),
    }),
  );
  const budgets = budgetSchema.parse(
    JSON.parse(
      (await run(cli, ["budget", "show", "--db", app.db])).stdout,
    ) as unknown,
  );
  expect(budgets.find((b) => b.team_id === "small")).toMatchObject({
    reserved_ticks: 200,
    remaining_ticks: 0,
  });
  await create(page, "cache-baseline", "baseline");
  await create(page, "cache-candidate", "candidate");
  const url = `${app.url}#view=compare&baseline=cache-baseline&candidate=cache-candidate`;
  await page.goto(url);
  await expect(
    page.getByText(/Live comparison: some selected tests are incomplete/),
  ).toBeVisible();
  await app.processJobs();
  await expect(
    page.getByText(/Saved comparison reused|Comparison saved for these inputs/),
  ).toBeVisible();
  await page.reload();
  await expect(page.getByText(/Saved comparison reused/)).toBeVisible();
  await page.getByLabel("Show groups").selectOption("regressions");
  await page.getByLabel("Group order").selectOption("id-desc");
  await page.getByRole("button", { name: "Compare requests" }).click();
  await expect(page.getByText(/1 groups on this page · 1 match/)).toBeVisible();
  await expect(page.getByText(/2 total groups: 2 compared/)).toBeVisible();
  await accessible(page);
  await page.screenshot({
    path: testInfo.outputPath("saved-comparison.png"),
    fullPage: true,
  });
  await page
    .getByRole("link", { name: "Candidate: stopped-obstacle", exact: true })
    .click();
  await page.getByRole("link", { name: "Back to comparison" }).click();
  await expect(page.getByLabel("Show groups")).toHaveValue("regressions");
  await expect(page.getByLabel("Group order")).toHaveValue("id-desc");
  // Pages, sort and filters each have separate cache identities.
  const schema = z.object({
    rows: z.array(z.object({ id: z.string() })),
    counts: z.object({ rows: z.number() }),
    view: z.object({
      cache_state: z.string(),
      cache_key: z.string(),
      next_after: z.string().optional(),
    }),
  });
  const compare = async (args: string[]) =>
    schema.parse(
      JSON.parse(
        (
          await run(cli, [
            "compare",
            "--db",
            app.db,
            "--baseline",
            "cache-baseline",
            "--candidate",
            "cache-candidate",
            "--save",
            "--duckdb",
            join(root, "bin/duckdb"),
            "--limit",
            "1",
            ...args,
          ])
        ).stdout,
      ) as unknown,
    );
  const first = await compare([]);
  const second = await compare(["--after", first.view.next_after ?? ""]);
  expect(first.rows).toHaveLength(1);
  expect(second.rows).toHaveLength(1);
  expect(first.rows[0]?.id).not.toBe(second.rows[0]?.id);
  expect(first.view.cache_key).not.toBe(second.view.cache_key);
  expect(first.counts.rows).toBe(2);
  expect(second.counts.rows).toBe(2);
  const hit = await compare(["--duckdb", "/unavailable-query-engine"]);
  expect(hit.view.cache_state).toBe("hit");
  await page.setViewportSize({ width: 390, height: 844 });
  await accessible(page);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});
