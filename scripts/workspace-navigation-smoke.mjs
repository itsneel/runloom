// Synthetic sessions and mocked APIs only; never changes real team data.
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
  nativeDialogs = [];
async function expectSingleNavigationToggle(label) {
  const toggles = page.getByRole("button", {
    name: /^(Toggle workspace sidebar|Collapse sidebar|Expand sidebar|Close sidebar)$/,
  });
  assert.equal(await toggles.count(), 1, "Only one accessible sidebar toggle at a time");
  assert.equal(await toggles.getAttribute("aria-label"), label);
  assert.equal(await page.locator('.content-header button[aria-controls="workspace-sidebar"], .editor-sidebar-toggle, runloom-workflow-dashboard .navigation-toggle').count(),
    label === "Toggle workspace sidebar" ? 1 : 0,
    "Top-bar toggle must be absent while the sidebar control is available");
}
page.on("pageerror", (e) => errors.push(e.message));
page.on("dialog", async (d) => {
  nativeDialogs.push(d.type());
  await d.dismiss();
});
const profile = {
  user: { id: "admin", name: "Alex Morgan", email: "alex@example.test" },
  organizations: [
    {
      id: "org",
      name: "Acme workspace",
      role: "owner",
      teams: [
        { id: "team", name: "Product", role: "admin" },
        { id: "ops", name: "Operations", role: "admin" },
      ],
    },
  ],
};
const members = {
  team: [
    { email: "alex@example.test", role: "admin", user_id: "alex" },
    { email: "sam@example.test", role: "editor", user_id: "sam" },
  ],
  ops: [{ email: "ops@example.test", role: "viewer", user_id: "" }],
};
let delayedTeam, releaseTeam, teamStarted;
await page.route("**/api/**", async (route) => {
  const path = new URL(route.request().url()).pathname,
    method = route.request().method();
  if (path === "/api/v1/auth/config")
    return route.fulfill({ status: 503, json: {} });
  if (path === "/api/v1/health")
    return route.fulfill({ json: { temporal: true, database: true } });
  if (path === "/api/v1/studio/me") return route.fulfill({ json: profile });
  if (path === "/api/v1/studio/projects")
    return route.fulfill({
      json: [
        { id: "default", name: "Customer experience" },
        { id: "onboarding", name: "Onboarding" },
      ],
    });
  if (path === "/api/v1/studio/definitions") return route.fulfill({ json: [] });
  if (path === "/api/v1/studio/orgs/org/teams" && method === "POST") {
    const team = {
      id: "new-team",
      name: route.request().postDataJSON().name,
      role: "admin",
    };
    profile.organizations[0].teams.push(team);
    members[team.id] = [];
    return route.fulfill({ json: team });
  }
  if (path === "/api/v1/studio/orgs/org/members")
    return route.fulfill({
      json: [{ email: "alex@example.test", role: "owner", user_id: "alex" }],
    });
  const match = path.match(/\/teams\/([^/]+)\/members$/);
  if (match) {
    const team = match[1];
    if (method === "POST") {
      members[team].push({ ...route.request().postDataJSON(), user_id: "" });
      return route.fulfill({ json: {} });
    }
    if (team === "team" && delayedTeam) {
      teamStarted?.();
      await delayedTeam;
    }
    return route.fulfill({ json: members[team] || [] });
  }
  if (path.endsWith("/secrets"))
    return route.fulfill({ json: { configured: true, items: [] } });
  return route.fulfill({ json: [] });
});
async function shell(code) {
  return page.evaluate((code) => {
    const shell = window.ng.getComponent(document.querySelector("app-root"));
    const result = eval(code);
    shell.cd.markForCheck();
    return result;
  }, code);
}
try {
  await page.goto(process.env.RUNLOOM_UI_URL || "http://localhost:4200");
  await page.waitForFunction(() =>
    window.ng?.getComponent(document.querySelector("app-root"))?.auth.ready(),
  );
  await page.evaluate((profile) => {
    const shell = window.ng.getComponent(document.querySelector("app-root"));
    shell.error = "";
    shell.auth.bearer = async () => "synthetic-token";
    shell.auth.profile.set(profile);
    shell.auth.organizationId.set("org");
    shell.auth.teamId.set("team");
    shell.auth.signedIn.set(true);
  }, profile);
  const sidebar = page.locator("aside.workspace-sidebar");
  await sidebar
    .getByRole("button", { name: "Workflows", exact: true })
    .waitFor();
  for (const name of ["Teams & members", "Organization", "Cost analytics", "LLM budgets", "Service accounts", "Workflow secrets"]) {
    assert.equal(await sidebar.getByRole("button", {name, exact:true}).count(), 1, name + " remains in the stable Management section");
  }
  assert.equal(await sidebar.getByRole("combobox", {name:"Organization", exact:true}).isVisible(), false);
  assert.equal(await sidebar.getByRole("heading", {name:"Management", exact:true}).count(), 1);
  assert.equal(
    await page.locator(".nav-rail").count(),
    0,
    "Only one navigation sidebar",
  );
  await page.getByLabel("Browse project", {exact: true}).selectOption("onboarding");
  assert.equal(await page.getByLabel("Browse project", {exact: true}).inputValue(), "onboarding");
  await page.getByRole("button", {name: "New workflow", exact: true}).click();
  await page
    .getByLabel("Workflow name", { exact: true })
    .fill("Customer welcome");
  await expectSingleNavigationToggle("Collapse sidebar");
  await page
    .getByRole("button", { name: "Collapse sidebar", exact: true })
    .click();
  await sidebar.waitFor({ state: "visible" });
  await sidebar
    .getByRole("button", { name: "Expand sidebar", exact: true })
    .waitFor();
  assert.equal(await sidebar.getAttribute("inert"), null);
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await expectSingleNavigationToggle("Expand sidebar");
  assert.equal(
    await sidebar.locator('select[aria-label="Organization"]').isVisible(),
    false,
  );
  assert.equal(
    await sidebar
      .getByRole("button", { name: "Workflows", exact: true })
      .isVisible(),
    true,
  );
  const teamsIcon = sidebar.getByRole("button", {
    name: "Teams & members",
    exact: true,
  });
  await teamsIcon.hover();
  await page
    .getByRole("tooltip", { name: "Teams & members", exact: true })
    .waitFor();
  const tooltipBox = await page.getByRole("tooltip").boundingBox();
  assert(
    tooltipBox.x >= 64,
    "Tooltip must render outside the rail without clipping",
  );
  await page.getByRole("tooltip").hover();
  await page.waitForTimeout(180);
  assert.equal(
    await page.getByRole("tooltip").isVisible(),
    true,
    "Tooltip remains hoverable",
  );
  await page.screenshot({ path: "/tmp/runloom-sidebar-collapsed.png" });
  await teamsIcon.focus();
  await page
    .getByRole("tooltip", { name: "Teams & members", exact: true })
    .waitFor();
  await page.keyboard.press("Escape");
  await page.getByRole("tooltip").waitFor({ state: "hidden" });
  await page.keyboard.press("Enter");
  await page.locator("runloom-dialogs").getByTestId("dialog-cancel").click();
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  assert.equal(
    await page.getByLabel("Workflow name", { exact: true }).inputValue(),
    "Customer welcome",
  );
  const switcher = page.locator(".content-header").getByRole("button", {name: "Switch organization or team"});
  await switcher.click();
  await page.waitForFunction(
    () => document.activeElement?.getAttribute("aria-label") === "Organization",
  );
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await page.keyboard.press("Escape");
  assert(await switcher.evaluate(el => el === document.activeElement));
  await sidebar
    .getByRole("button", { name: "Projects", exact: true })
    .click();
  await page.locator("runloom-dialogs").getByTestId("dialog-cancel").click();
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await sidebar.getByRole("button", {name:"Expand sidebar", exact:true}).click();
  assert.equal(await switcher.getAttribute("aria-expanded"), "false");
  await page.screenshot({ path: "/tmp/runloom-workspace-builder.png" });
  await page
    .getByRole("button", { name: "Teams & members", exact: true })
    .click();
  const modal = page.locator("runloom-dialogs").getByRole("dialog");
  await modal.getByTestId("dialog-cancel").click();
  assert.equal(
    await page.getByLabel("Workflow name", { exact: true }).inputValue(),
    "Customer welcome",
  );
  await page
    .getByRole("button", { name: "Teams & members", exact: true })
    .click();
  await modal.getByTestId("dialog-confirm").click();
  await page
    .getByRole("heading", { name: "Teams & access", exact: true })
    .waitFor();
  assert.equal(await sidebar.getByRole("button", {name:"Management", exact:true}).count(), 0);
  await sidebar.getByRole("button", {name:"Cost analytics", exact:true}).waitFor();
  await page.getByText("sam@example.test", { exact: true }).waitFor();
  await page.getByLabel("Search team members").fill("sam");
  assert.equal(await page.locator(".member-row").count(), 1);
  await page.getByLabel("Search team members").fill("");
  await page.screenshot({ path: "/tmp/runloom-workspace-teams.png" });
  await page
    .getByRole("button", { name: "Manage team Operations", exact: true })
    .click();
  await page.getByText("ops@example.test", { exact: true }).waitFor();
  assert.equal(
    await page.getByText("sam@example.test", { exact: true }).count(),
    0,
  );
  await page
    .getByLabel("Team member email", { exact: true })
    .fill("new@example.test");
  await page.getByRole("button", { name: "Add to team", exact: true }).click();
  await page.getByText("new@example.test", { exact: true }).waitFor();
  await page.getByLabel("New team name").fill("Customer success");
  await page.getByRole("button", { name: "Create team", exact: true }).click();
  await page
    .getByRole("button", { name: "Manage team Customer success", exact: true })
    .waitFor();
  assert.equal(
    await page.getByLabel("Team", { exact: true }).inputValue(),
    "new-team",
  );
  await page.getByRole("button", { name: "Organization", exact: true }).click();
  await page
    .getByRole("heading", { name: "Organization members", exact: true })
    .waitFor();
  assert.equal(
    await page.getByLabel("Team member email", { exact: true }).count(),
    0,
    "Separate organization and team pages",
  );
  await page
    .getByRole("button", { name: "Service accounts", exact: true })
    .click();
  await page.locator("runloom-service-accounts").waitFor();
  assert.equal(await page.locator("runloom-workflow-secrets").count(), 0);
  await page
    .getByRole("button", { name: "Workflow secrets", exact: true })
    .click();
  await page.locator("runloom-workflow-secrets").waitFor();
  assert.equal(await page.locator("runloom-service-accounts").count(), 0);
  await page
    .getByRole("button", { name: "Teams & members", exact: true })
    .click();
  const started = new Promise((resolve) => {
    teamStarted = resolve;
  });
  delayedTeam = new Promise((resolve) => {
    releaseTeam = resolve;
  });
  await shell("void (window.pendingTeam = shell.selectTeam('team'))");
  await started;
  await shell("void shell.selectTeam('ops')");
  await page.getByText("ops@example.test", { exact: true }).waitFor();
  releaseTeam();
  delayedTeam = undefined;
  await page.evaluate(() => window.pendingTeam);
  await page.waitForFunction(
    () => !window.ng.getComponent(document.querySelector("app-root")).busy,
  );
  assert.equal(
    await page.getByText("sam@example.test", { exact: true }).count(),
    0,
    "Late team response must not leak into current team",
  );
  await sidebar
    .getByRole("button", { name: "Collapse sidebar", exact: true })
    .click();
  await page.setViewportSize({ width: 390, height: 844 });
  await sidebar.waitFor({ state: "hidden" });
  await page.waitForFunction(
    () => document.querySelector("aside.workspace-sidebar").inert,
  );
  assert.equal(await sidebar.getAttribute("inert"), "");
  await expectSingleNavigationToggle("Toggle workspace sidebar");
  await page
    .getByRole("button", { name: "Toggle workspace sidebar", exact: true })
    .click();
  await page.waitForFunction(
    () => document.querySelector(".workspace-content").inert,
  );
  assert.equal(
    await page.locator(".workspace-content").getAttribute("inert"),
    "",
  );
  await expectSingleNavigationToggle("Close sidebar");
  await sidebar.getByRole("button", { name: "Close sidebar" }).focus();
  await page.keyboard.press("Shift+Tab");
  assert.equal(
    await page
      .getByRole("button", { name: "Sign out", exact: true })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await page.keyboard.press("Tab");
  assert.equal(
    await sidebar
      .getByRole("button", { name: "Close sidebar" })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  await page.screenshot({ path: "/tmp/runloom-workspace-mobile.png" });
  await page.keyboard.press("Escape");
  await sidebar.waitFor({ state: "hidden" });
  assert.equal(
    await page
      .getByRole("button", { name: "Toggle workspace sidebar" })
      .evaluate((el) => el === document.activeElement),
    true,
  );
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.getByRole("button", { name: "Toggle workspace sidebar" }).click();
  await page
    .getByRole("button", { name: "Workflows", exact: true })
    .click();
  await sidebar.waitFor({ state: "hidden" });
  assert.equal(await sidebar.getAttribute("inert"), "");
  await expectSingleNavigationToggle("Toggle workspace sidebar");
  await page.getByRole("button", { name: "Toggle workspace sidebar" }).click();
  await sidebar
    .getByRole("button", { name: "Workflows", exact: true })
    .waitFor();
  await expectSingleNavigationToggle("Close sidebar");
  await sidebar.getByRole("button", { name: "Close sidebar", exact: true }).click();
  await sidebar.waitFor({ state: "hidden" });
  await expectSingleNavigationToggle("Toggle workspace sidebar");
  assert.equal(await page.getByRole("button", { name: "Toggle workspace sidebar" }).evaluate(el => el === document.activeElement), true);
  await page.getByRole("button", { name: "Toggle workspace sidebar" }).click();
  await page
    .getByRole("button", { name: "Close navigation", exact: true })
    .click({ position: { x: 350, y: 30 } });
  await sidebar.waitFor({ state: "hidden" });
  assert.equal(await sidebar.getAttribute("inert"), "");
  await page.setViewportSize({ width: 1440, height: 1000 });
  await sidebar
    .getByRole("button", { name: "Expand sidebar", exact: true })
    .waitFor();
  assert.equal(await sidebar.getAttribute("inert"), null);
  assert.equal(Math.round((await sidebar.boundingBox()).width), 64);
  await sidebar
    .getByRole("button", { name: "Expand sidebar", exact: true })
    .click();
  await shell(
    "shell.auth.profile.update(p=>({...p,organizations:[{...p.organizations[0],role:'member',teams:p.organizations[0].teams.map(t=>({...t,role:'viewer'}))}]}))",
  );
  await page.waitForFunction(
    () => document.querySelector("runloom-workflow-dashboard .primary")?.disabled,
  );
  assert.equal(
    await page
      .getByRole("button", { name: "New workflow", exact: true })
      .isDisabled(),
    true,
  );
  assert.equal(
    await sidebar
      .getByRole("button", { name: "Service accounts", exact: true })
      .count(),
    0,
  );
  await page
    .getByRole("button", { name: "Teams & members", exact: true })
    .click();
  await page
    .getByRole("heading", { name: "Teams & access", exact: true })
    .waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "Create team", exact: true })
      .count(),
    0,
  );
  assert.equal(
    await page
      .getByRole("button", { name: "Add to team", exact: true })
      .count(),
    0,
  );
  await sidebar
    .getByRole("button", { name: "Collapse sidebar", exact: true })
    .click();
  await sidebar
    .getByRole("button", { name: "Expand sidebar", exact: true })
    .waitFor();
  await expectSingleNavigationToggle("Expand sidebar");
  assert.equal(
    await sidebar
      .getByRole("button", { name: "Service accounts", exact: true })
      .count(),
    0,
  );
  assert.equal(
    await sidebar
      .getByRole("button", { name: "Workflow secrets", exact: true })
      .count(),
    0,
  );
  await page.setViewportSize({ width: 1024, height: 420 });
  await sidebar.getByRole("button", { name: "Sign out", exact: true }).hover();
  await page.getByRole("tooltip", { name: "Sign out", exact: true }).waitFor();
  const compactTooltip = await page.getByRole("tooltip").boundingBox();
  assert(
    compactTooltip.y >= 0 && compactTooltip.y + compactTooltip.height <= 420,
  );
  await sidebar
    .getByRole("button", { name: "Expand sidebar", exact: true })
    .click();
  await sidebar
    .getByRole("button", { name: "Collapse sidebar", exact: true })
    .waitFor();
  assert.deepEqual(errors, []);
  assert.deepEqual(nativeDialogs, []);
  console.log(
    "PASS single sidebar, projects, persistent icon rail, hover/focus tooltips, compact viewport, unsaved guards, team switching, member search/add, team creation, dedicated settings, stale responses, mobile focus/Escape/backdrop, viewer permissions",
  );
} finally {
  releaseTeam?.();
  await browser.close();
}
