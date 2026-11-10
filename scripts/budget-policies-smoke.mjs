// Synthetic sessions and mocked API; never changes actual budget policies.
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
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, colorScheme: process.env.RUNLOOM_COLOR_SCHEME || 'light' });
page.setDefaultTimeout(10000);
const errors = [],
  writes = [];
let failSave = false,
  failRead = false;
page.on("pageerror", (e) => errors.push(e.message));
const report = {
  items: [],
  projects: [
    { id: "default", name: "General" },
    { id: "second", name: "Operations" },
  ],
  workflows: [{ id: "welcome", name: "Welcome workflow" }],
  spend: [
    {
      project_id: "default",
      project_name: "General",
      spend_usd: "125",
      incomplete: false,
      requests: 20,
    },
    {
      project_id: "second",
      project_name: "Operations",
      spend_usd: "10",
      incomplete: true,
      requests: 8,
    },
  ],
  alerts: [],
  events: [],
  changes: [],
  month: "2026-10",
  resets_at: "2026-11-01T00:00:00Z",
};
await page.route("**/api/**", async (route) => {
  const r = route.request(),
    url = new URL(r.url());
  if (url.pathname === "/api/v1/auth/config")
    return route.fulfill({ status: 503, json: {} });
  if (url.pathname === "/api/v1/studio/projects")
    return route.fulfill({ json: report.projects });
  if (url.pathname.includes("/llm-budget-policies")) {
    if (r.method() === "PUT") {
      const body = r.postDataJSON();
      writes.push(body);
      if (failSave)
        return route.fulfill({
          status: 409,
          json: { detail: "Policy changed; refresh before saving." },
        });
      const saved = {
        ...body,
        revision: body.revision + 1,
        updated_at: "2026-10-02T10:00:00Z",
      };
      report.items = [saved];
      report.changes = [{ actor: "admin", policy: saved }];
      return route.fulfill({ json: saved });
    }
    return failRead
      ? route.fulfill({
          status: 503,
          json: {
            detail:
              "Monthly budget accounting unavailable; no partial totals returned",
          },
        })
      : route.fulfill({ json: report });
  }
  if (url.pathname === "/api/v1/studio/runs/paused/resume")
    return route.fulfill({ json: { accepted: true } });
  return route.fulfill({ json: [] });
});
try {
  await page.goto(process.env.RUNLOOM_UI_URL || "http://localhost:14201");
  await page.waitForFunction(() =>
    window.ng?.getComponent(document.querySelector("app-root"))?.auth.ready(),
  );
  await page.evaluate(() => {
    const root = window.ng.getComponent(document.querySelector("app-root"));
    root.error = "";
    root.auth.bearer = async () => "synthetic";
    root.auth.profile.set({
      user: { id: "admin", name: "Alex", email: "alex@example.test" },
      organizations: [
        {
          id: "org",
          name: "Runloom test",
          role: "owner",
          teams: [{ id: "team", name: "Product", role: "admin" }],
        },
      ],
    });
    root.auth.organizationId.set("org");
    root.auth.teamId.set("team");
    root.auth.signedIn.set(true);
  });
  await page.getByRole("button", { name: "Management", exact: true }).click();
  await page.getByRole("button", { name: "LLM budgets", exact: true }).click();
  const budgets = page.locator("runloom-budget-policies");
  await budgets
    .getByRole("heading", { name: "Project budgets · 2026-10", exact: true })
    .waitFor();
  assert.equal(
    await budgets.locator("tbody tr").first().locator("td").first().innerText(),
    "General",
  );
  await budgets
    .getByRole("button", { name: "Recorded spend", exact: true })
    .click();
  await page.waitForFunction(
    () =>
      document
        .querySelector("runloom-budget-policies tbody tr td")
        ?.textContent?.trim() === "Operations",
  );
  assert.equal(
    await budgets.locator("tbody tr").first().locator("td").first().innerText(),
    "Operations",
  );
  await budgets
    .getByRole("button", { name: "New policy", exact: true })
    .click();
  await budgets
    .getByLabel("Policy name", { exact: true })
    .fill("Monthly guardrails");
  await budgets.getByLabel("Soft limit USD", { exact: true }).fill("100");
  await budgets.getByLabel("Hard limit USD", { exact: true }).fill("200");
  assert.equal(
    await budgets
      .getByLabel("Apply automatically to all current and future projects")
      .isChecked(),
    true,
  );
  await budgets
    .locator("summary")
    .filter({ hasText: "Workflow exclusions" })
    .click();
  await budgets.getByLabel("Exclude Welcome workflow").check();
  await budgets
    .getByLabel("Reason for change", { exact: true })
    .fill("Protect project budgets");
  await page
    .getByRole("button", { name: "Cost analytics", exact: true })
    .click();
  await page.locator("runloom-dialogs").getByTestId("dialog-cancel").click();
  assert.equal(
    await budgets.getByLabel("Policy name", { exact: true }).inputValue(),
    "Monthly guardrails",
  );
  await budgets
    .getByRole("button", { name: "Save policy", exact: true })
    .click();
  await budgets
    .getByRole("button", {
      name: "Edit policy Monthly guardrails",
      exact: true,
    })
    .waitFor();
  assert.deepEqual(writes[0].projects, []);
  assert.deepEqual(writes[0].excluded_workflows, ["welcome"]);
  assert.equal(writes[0].revision, 0);
  await page.screenshot({ path: "/tmp/runloom-budget-policies.png" });
  await budgets
    .getByRole("button", {
      name: "Edit policy Monthly guardrails",
      exact: true,
    })
    .click();
  await budgets
    .getByLabel("Apply automatically to all current and future projects")
    .uncheck();
  await budgets
    .getByLabel("Reason for change", { exact: true })
    .fill("Narrow scope");
  await budgets
    .getByRole("button", { name: "Save policy", exact: true })
    .click();
  await budgets
    .getByText("Select at least one project, or apply to all projects.")
    .waitFor();
  assert.equal(writes.length, 1);
  await budgets.getByLabel("Operations", { exact: true }).check();
  failSave = true;
  await budgets
    .getByRole("button", { name: "Save policy", exact: true })
    .click();
  await budgets.getByText("Policy changed; refresh before saving.").waitFor();
  assert.deepEqual(writes[1].projects, ["second"]);
  assert.equal(writes[1].revision, 1);
  await budgets
    .getByRole("button", { name: "Cancel editing", exact: true })
    .click();
  await page.locator("runloom-dialogs").getByTestId("dialog-confirm").click();
  failRead = true;
  await budgets
    .getByRole("button", { name: "Refresh budgets", exact: true })
    .click();
  await budgets
    .getByText(
      "Monthly budget accounting unavailable; no partial totals returned",
    )
    .waitFor();
  assert.equal(await budgets.locator("table").count(), 0);
  failRead = false;
  await budgets
    .getByRole("button", { name: "Refresh budgets", exact: true })
    .click();
  await budgets.locator("table").waitFor();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: "/tmp/runloom-budget-policies-mobile.png" });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.evaluate(() => {
    const root = window.ng.getComponent(document.querySelector("app-root"));
    root.auth.profile.update((p) => ({
      ...p,
      organizations: p.organizations.map((o) => ({
        ...o,
        role: "member",
        teams: o.teams.map((t) => ({ ...t, role: "viewer" })),
      })),
    }));
  });
  await page.waitForFunction(
    () =>
      !window.ng
        .getComponent(document.querySelector("app-root"))
        .auth.teamAdmin(),
  );
  assert.equal(
    await budgets
      .getByRole("button", { name: "New policy", exact: true })
      .count(),
    0,
  );
  assert.equal(
    await budgets
      .getByRole("button", {
        name: "Edit policy Monthly guardrails",
        exact: true,
      })
      .count(),
    0,
  );
  await page
    .getByRole("button", { name: "Workflows", exact: true })
    .click();
  await page.locator("runloom-studio").waitFor();
  await page.evaluate(() => {
    const app = window.ng.getComponent(
      document.querySelector("runloom-studio"),
    );
    app.runOpen = true;
    app.run = {
      workflow_id: "paused",
      temporal_status: "running",
      state: {
        status: "paused",
        steps: {},
        paused_step: {
          step_id: "llm",
          message:
            "Monthly guardrails: project General reached its hard limit.",
          budget: {
            reason: "hard_limit_reached",
            resets_at: "2026-11-01T00:00:00Z",
            incomplete: true,
          },
        },
      },
    };
    window.ng.applyChanges(app);
  });
  await page.getByText("LLM budget pause", { exact: true }).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "Recheck budget & resume", exact: true })
      .isDisabled(),
    true,
  );
  await page
    .getByText(
      "This workflow is paused, not canceled or completed. Its progress is retained.",
    )
    .waitFor();
  assert.deepEqual(errors, []);
  console.log(
    "PASS budget policies, inheritance, exclusions, per-project sortable spend, unsaved guards, conflict/errors, viewer access, mobile and pause reason",
  );
} finally {
  await browser.close();
}
