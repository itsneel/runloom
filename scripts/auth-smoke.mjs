// Local development only: creates a synthetic account and leaves its records.
// Requires Playwright and a Chrome/Chromium executable. Never logs credentials.
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { setTimeout as delay } from "node:timers/promises";
import { spawn } from "node:child_process";
const { chromium } = await import(
  process.env.RUNLOOM_PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.RUNLOOM_UI_URL || "http://localhost:4200";
const mail = process.env.RUNLOOM_MAILPIT_URL || "http://localhost:8025";
const email = "auth-" + randomUUID() + "@example.test";
const password = "Test!" + randomUUID();
const newPassword = "Reset!" + randomUUID();
const browser = await chromium.launch({
  headless: true,
  ...(process.env.RUNLOOM_CHROME
    ? { executablePath: process.env.RUNLOOM_CHROME }
    : {}),
});
const page = await browser.newPage();
let token;
page.on("response", async (response) => {
  if (
    response.url().endsWith("/protocol/openid-connect/token") &&
    response.ok()
  ) {
    token = (await response.json()).access_token;
  }
});
async function emailLink(subject) {
  for (let i = 0; i < 40; i++) {
    const { messages = [] } = await (
      await fetch(mail + "/api/v1/messages")
    ).json();
    const msg = messages.find(
      (m) =>
        m.To?.some((to) => to.Address === email) &&
        m.Subject.toLowerCase().includes(subject),
    );
    if (msg) {
      const body = await (
        await fetch(mail + "/api/v1/message/" + msg.ID)
      ).json();
      const link = (body.Text || "").match(/https?:\/\/[^\s<>]+/);
      assert(link, "Email must contain an action link");
      return link[0];
    }
    await delay(500);
  }
  throw new Error("Expected email did not arrive: " + subject);
}
async function api(path, body, team) {
  assert(token, "Expected access token");
  const response = await fetch(base + path, {
    method: body ? "POST" : "GET",
    headers: {
      Authorization: "Bearer " + token,
      "Content-Type": "application/json",
      ...(team ? { "X-Runloom-Team": team } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  assert(response.ok, path + ": HTTP " + response.status);
  return response.json();
}
try {
  await page.goto(base);
  await page.locator('#username').waitFor();
  assert.equal(await page.title(), "Sign in to Runloom");
  await page.getByRole('link', { name: 'Create an account', exact: true }).click();
  await page.locator("#email").fill(email);
  await page.locator("#password").fill(password);
  await page.locator("#password-confirm").fill(password);
  await page.locator("#firstName").fill("Auth");
  await page.locator("#lastName").fill("Smoke");
  await page.locator("input[type=submit], button[type=submit]").click();
  await page.goto(await emailLink("verif"));
  await page.getByRole('button', { name: 'Set up organization', exact: true }).click();
  await page.getByRole('textbox', { name: 'Organization name', exact: true }).waitFor();
  console.log("PASS registration, email verification, and PKCE sign-in");
  await page
    .getByRole("textbox", { name: "Organization name", exact: true })
    .fill("Auth smoke organization");
  await page
    .getByRole("button", { name: "Create organization", exact: true })
    .click();
  console.log("Organization creation submitted");
  await page.locator("runloom-studio").waitFor();
  await page
    .getByRole("button", { name: "Management", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "New team name", exact: true })
    .fill("Engineering");
  await page.getByRole("button", { name: "Create team", exact: true }).click();
  await page.waitForFunction(() =>
    document
      .querySelector('select[aria-label="Team"]')
      ?.selectedOptions[0]?.textContent?.includes("Engineering"),
  );
  const profile = await api("/api/v1/studio/me");
  const org = profile.organizations.find(
    (o) => o.name === "Auth smoke organization",
  );
  const team = org.teams.find((t) => t.name === "Engineering");
  assert.equal(org.role, "owner");
  assert.equal(team.role, "admin");
  await page.goto(base + '/?project=auth-smoke#workflow');
  await page.locator('runloom-studio').waitFor();
  assert.equal(new URL(page.url()).searchParams.get('project'), 'auth-smoke');
  assert.equal(new URL(page.url()).hash, '#workflow');
  console.log('PASS existing session skips login and retains the Studio URL');
  await api("/api/v1/studio/orgs/" + org.id + "/members", {
    email: "pending-" + randomUUID() + "@example.test",
    role: "member",
  });
  console.log("PASS organization/team creation and pending membership");
  await new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ["scripts/smoke.mjs"], {
      stdio: "inherit",
      env: {
        ...process.env,
        RUNLOOM_UI_URL: base,
        RUNLOOM_ACCESS_TOKEN: token,
        RUNLOOM_TEAM_ID: team.id,
      },
    });
    child.on("error", reject);
    child.on("exit", (code) =>
      code === 0 ? resolve() : reject(new Error("Workflow smoke failed")),
    );
  });
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page.locator('#username').waitFor();
  assert.equal(await page.title(), 'Sign in to Runloom');
  // A fresh Studio navigation must not silently reuse the terminated session.
  await page.goto(base);
  await page.locator('#username').waitFor();
  console.log('PASS logout returns directly to branded login and ends the session');
  await page
    .getByRole("link", { name: /forgot.*password/i })
    .click();
  await page.locator("#username").fill(email);
  await page.locator("input[type=submit], button[type=submit]").click();
  await page.goto(await emailLink("reset"));
  await page.locator("#password-new").fill(newPassword);
  await page.locator("#password-confirm").fill(newPassword);
  await page.locator("input[type=submit], button[type=submit]").click();
  console.log("Password reset submitted");
  await page.waitForTimeout(1000);
  await page.goto(base);
  const logout = page.getByRole("button", { name: "Sign out", exact: true });
  await logout.or(page.locator('#username')).waitFor();
  if (await logout.isVisible()) await logout.click();
  await page.locator('#username').waitFor();
  console.log("Checking password replacement");
  await page.locator("#username").fill(email);
  await page.locator("#password").fill(password);
  await page.locator("#kc-login").click();
  await page
    .getByText("Invalid username or password.", { exact: true })
    .waitFor();
  console.log("Old password rejected");
  await page.locator("#password").fill(newPassword);
  await page.locator("#kc-login").click();
  await page.getByRole("button", { name: "Sign out", exact: true }).waitFor();
  console.log(
    "PASS password reset, old password rejected, and new password login",
  );
} catch (error) {
  const alerts = await page.getByRole("alert").allTextContents();
  if (alerts.length) console.error("Visible error:", alerts.join(" "));
  // Browser error stacks can include action-link URLs; do not print them.
  throw new Error("Account browser smoke failed (" + error.name + ").");
} finally {
  await browser.close();
}
