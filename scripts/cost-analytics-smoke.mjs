// Synthetic browser sessions and mocked analytics. No paid LLM calls or writes.
import assert from "node:assert/strict";
const { chromium } = await import(
  process.env.RUNLOOM_PLAYWRIGHT_MODULE || "playwright"
);
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME
    ? { executablePath: process.env.RUNLOOM_CHROME }
    : {}),
});
const page = await browser.newPage({ viewport: { width: 1600, height: 1100 }, colorScheme: process.env.RUNLOOM_COLOR_SCHEME || 'light' });
page.setDefaultTimeout(10000);
const errors = [],
  queries = [];
let state = "normal",
  release,
  started;
page.on("pageerror", (e) => errors.push(e.message));
const metrics = {
  estimated_usd: "125.350000000000",
  average_usd: "2.005600000000",
  requests: 144,
  priced: 130,
  unknown: 4,
  unpriced: 8,
  pending: 2,
  runs: 64,
  priced_runs: 62,
  input_tokens: 520000,
  output_tokens: 125000,
  failed_requests: 3,
  retry_requests: 6,
};
const empty = {
  estimated_usd: "0.000000000000",
  average_usd: null,
  requests: 0,
  priced: 0,
  unknown: 0,
  unpriced: 0,
  pending: 0,
  runs: 0,
  priced_runs: 0,
  input_tokens: 0,
  output_tokens: 0,
  failed_requests: 0,
  retry_requests: 0,
};
await page.route("**/api/**", async (route) => {
  const url = new URL(route.request().url());
  if (url.pathname === "/api/v1/auth/config")
    return route.fulfill({ status: 503, json: {} });
  if (url.pathname === "/api/v1/studio/projects")
    return route.fulfill({ json: [{ id: "default", name: "General" }] });
  if (url.pathname.endsWith("/llm-analytics")) {
    const query = url.searchParams;
    queries.push(query);
    if (state === "error")
      return route.fulfill({
        status: 503,
        json: { detail: "Analytics storage unavailable." },
      });
    if (state === "delayed" && url.pathname.includes("/org/")) {
      started?.();
      await new Promise((resolve) => {
        release = resolve;
      });
    }
    const other = url.pathname.includes("/other/");
    const rows = Array.from(
      { length: query.get("page") === "1" ? 14 : 50 },
      (_, i) => ({
        ...metrics,
        id: "r" + i,
        name: (other ? "Other workspace" : "Customer welcome") + " " + i,
        team_name: "Product",
        workflow_id: "run-" + i,
        temporal_run_id: "temporal-" + i,
        definition_version: 2,
        last_request_at: "2026-10-02T09:00:00Z",
        requests: 2,
        runs: 1,
      }),
    );
    const trend = Array.from({ length: 14 }, (_, i) => ({
      ...metrics,
      id: "2026-09-" + String(i + 10).padStart(2, "0") + "T00:00:00Z",
      name: "day",
      estimated_usd: String((i + 1) * 0.81),
      requests: 10,
      priced: i === 3 ? 8 : 10,
    }));
    return route.fulfill({
      json: {
        window: {
          from: "2026-09-02T09:00:00Z",
          until: "2026-10-02T09:00:00Z",
          previous_from: "2026-08-03T09:00:00Z",
          bucket: "day",
        },
        summary: state === "empty" ? empty : metrics,
        previous: metrics,
        rows: state === "empty" ? [] : rows,
        trend: state === "empty" ? [] : trend,
        total_rows: state === "empty" ? 0 : 64,
        page_size: 50,
        as_of: "2026-10-02T09:00:00Z",
        allowed_teams: [
          {
            id: other ? "third" : "team",
            name: other ? "Other team" : "Product",
            role: "viewer",
          },
        ],
      },
    });
  }
  return route.fulfill({ json: [] });
});
const dashboard = page.locator("runloom-cost-analytics");
async function loaded() {
  await page.waitForFunction(
    () =>
      !window.ng.getComponent(document.querySelector("runloom-cost-analytics"))
        ?.busy,
  );
}
try {
  await page.goto(process.env.RUNLOOM_UI_URL || "http://localhost:14201");
  await page.waitForFunction(() =>
    window.ng?.getComponent(document.querySelector("app-root"))?.auth.ready(),
  );
  await page.evaluate(() => {
    const root = window.ng.getComponent(document.querySelector("app-root"));
    root.error = "";
    root.auth.bearer = async () => "synthetic-token";
    root.auth.profile.set({
      user: { id: "owner", name: "Alex Morgan", email: "alex@example.test" },
      organizations: [
        {
          id: "org",
          name: "Acme workspace",
          role: "owner",
          teams: [{ id: "team", name: "Product", role: "admin" }],
        },
        {
          id: "other",
          name: "Other organization",
          role: "member",
          teams: [{ id: "third", name: "Other team", role: "viewer" }],
        },
      ],
    });
    root.auth.organizationId.set("org");
    root.auth.teamId.set("team");
    root.auth.signedIn.set(true);
  });
  await page
    .getByLabel("Workflow name", { exact: true })
    .fill("Unsaved analytics navigation");
  await page
    .getByRole("button", { name: "Management", exact: true })
    .click();
  const modal = page.locator("runloom-dialogs").getByRole("dialog");
  await modal.getByTestId("dialog-cancel").click();
  assert.equal(
    await page.getByLabel("Workflow name", { exact: true }).inputValue(),
    "Unsaved analytics navigation",
  );
  await page
    .getByRole("button", { name: "Management", exact: true })
    .click();
  await modal.getByTestId("dialog-confirm").click();
  await page.getByRole("button", { name: "Cost analytics", exact: true }).click();
  await dashboard.locator(".main-kpi strong").waitFor();
  await loaded();
  assert.equal(
    await dashboard.locator(".main-kpi strong").innerText(),
    "$125.35",
  );
  await dashboard.getByText("Spend is incomplete.", { exact: true }).waitFor();
  await dashboard
    .getByText("Comparison unavailable or incomplete", { exact: true })
    .waitFor();
  for (const [label, id] of [
    ["1 day", "1d"],
    ["1 week", "1w"],
    ["1 month", "1m"],
    ["MTD", "mtd"],
    ["YTD", "ytd"],
    ["6 months", "6m"],
    ["1 year", "1y"],
  ]) {
    await dashboard.getByRole("button", { name: label, exact: true }).click();
    await loaded();
    assert.equal(queries.at(-1).get("period"), id);
  }
  await dashboard
    .getByRole("button", { name: "Estimated spend", exact: true })
    .click();
  await loaded();
  assert.equal(queries.at(-1).get("direction"), "asc");
  assert.equal(await dashboard.locator('th[aria-sort="ascending"]').count(), 1);
  await dashboard.getByRole("button", { name: "Next", exact: true }).click();
  await loaded();
  assert.equal(queries.at(-1).get("page"), "1");
  assert.equal(queries.at(-1).get("as_of"), "2026-10-02T09:00:00Z");
  assert.equal(await dashboard.locator(".breakdown-card tbody tr").count(), 14);
  await dashboard
    .getByRole("group", { name: "Breakdown level" })
    .getByRole("button", { name: "Teams", exact: true })
    .click();
  await loaded();
  assert.equal(queries.at(-1).get("group"), "teams");
  assert.equal(queries.at(-1).get("page"), "0");
  await dashboard
    .getByRole("group", { name: "Breakdown level" })
    .getByRole("button", { name: "Models", exact: true })
    .click();
  await loaded();
  assert.equal(queries.at(-1).get("group"), "models");
  await dashboard
    .getByLabel("Analytics team", { exact: true })
    .selectOption("team");
  await loaded();
  assert.equal(queries.at(-1).get("team"), "team");
  await dashboard
    .getByLabel("Analytics run classification")
    .selectOption("test");
  await loaded();
  assert.equal(queries.at(-1).get("mode"), "test");
  await dashboard
    .getByRole("group", { name: "Breakdown level" })
    .getByRole("button", { name: "Runs", exact: true })
    .click();
  await loaded();
  await page.screenshot({ path: "/tmp/runloom-cost-analytics.png" });
  await dashboard
    .getByText("View accessible trend data", { exact: true })
    .click();
  assert.equal(await dashboard.locator(".trend-data tbody tr").count(), 14);
  state = "empty";
  await dashboard.getByRole("button", { name: "Refresh", exact: true }).click();
  await loaded();
  await dashboard
    .getByRole("heading", { name: "No recorded LLM requests in this period" })
    .waitFor();
  assert.equal(await dashboard.locator(".main-kpi strong").innerText(), "—");
  state = "error";
  await dashboard.getByRole("button", { name: "Refresh", exact: true }).click();
  await loaded();
  await dashboard.getByRole("alert").waitFor();
  assert.equal(await dashboard.locator(".kpi").count(), 0);
  state = "delayed";
  const began = new Promise((resolve) => {
    started = resolve;
  });
  await dashboard
    .getByRole("button", { name: "Try again", exact: true })
    .click();
  await began;
  await page.getByRole("button", { name: "Switch organization or team", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Organization", exact: true })
    .selectOption("other");
  await dashboard.getByText("Other workspace 0", { exact: true }).waitFor();
  release();
  await loaded();
  assert.equal(
    await dashboard.getByText("Customer welcome 0", { exact: true }).count(),
    0,
    "Late results cannot cross an organization change",
  );
  assert.match(
    await dashboard.locator(".scope-controls p").innerText(),
    /Only your authorized teams/,
  );
  await page.setViewportSize({ width: 390, height: 844 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
    "Mobile page must not overflow",
  );
  await page.screenshot({ path: "/tmp/runloom-cost-analytics-mobile.png" });
  assert.deepEqual(errors, []);
  console.log(
    "PASS cost analytics KPIs, seven date ranges, scope/filter requests, server sorting/paging, trend table, incomplete/empty/error states, stale response protection, unsaved guard and mobile",
  );
} finally {
  release?.();
  await browser.close();
}
