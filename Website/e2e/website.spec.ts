import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { siteConfig } from "../src/app/content";

test("login opens the configured Studio from desktop and mobile navigation", async ({ page }) => {
  // Stub only the destination so this suite remains independent of Studio.
  await page.route(siteConfig.studioUrl, (route) => route.fulfill({
    contentType: "text/html",
    body: "<h1>Studio sign in</h1>",
  }));
  await page.goto("/");
  const menu = page.getByRole("button", { name: "Toggle navigation" });
  if (await menu.isVisible()) await menu.click();
  const login = page.getByRole("navigation", { name: "Main navigation" })
    .getByRole("link", { name: "Log in", exact: true });
  await expect(login).toBeVisible();
  await expect(login).toHaveAttribute("href", siteConfig.studioUrl);
  await login.click();
  await expect(page).toHaveURL(siteConfig.studioUrl);
  await expect(page.getByRole("heading", { name: "Studio sign in" })).toBeVisible();
});

test("home page and expanded JSON meet automated accessibility checks", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page).toHaveTitle(/Runloom/);
  await expect(page.getByRole("link", { name: "Runloom home" })).toContainText("Runloom");
  await page
    .locator("summary")
    .filter({ hasText: "Is the website demo running a real workflow?" })
    .click();
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
  await page.getByRole("button", { name: "View JSON" }).click();
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
});

test("loads independently with working navigation and no overflow", async ({
  page,
  baseURL,
}) => {
  const errors: string[] = [];
  const external: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("request", (request) => {
    if (!request.url().startsWith(baseURL!)) external.push(request.url());
  });
  await page.goto("/");
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "Turn your process",
  );
  await page.getByRole("link", { name: "See how it flows" }).click();
  await expect(page).toHaveURL(/#playground$/);
  await expect(
    page.getByRole("button", { name: "Run the demo" }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  if (
    await page.getByRole("button", { name: "Toggle navigation" }).isVisible()
  ) {
    await page.getByRole("button", { name: "Toggle navigation" }).click();
    await expect(
      page.getByRole("navigation", { name: "Main navigation" }),
    ).toBeVisible();
    await page
      .getByRole("navigation", { name: "Main navigation" })
      .getByRole("link", { name: "FAQs" })
      .click();
    await expect(
      page.getByRole("button", { name: "Toggle navigation" }),
    ).toHaveAttribute("aria-expanded", "false");
  }
  await page
    .locator("summary")
    .filter({ hasText: "Is the website demo running a real workflow?" })
    .click();
  await expect(
    page.getByText("No. It is a browser-only illustration", { exact: false }),
  ).toBeVisible();
  const license = await page.request.get("/LICENSE");
  expect(license.status()).toBe(200);
  expect(await license.text()).toContain("MIT License");
  expect(errors).toEqual([]);
  expect(external).toEqual([]);
});

test("approval and callback demos wait for user action and finish", async ({
  page,
}) => {
  await page.goto("/#playground");
  await page.getByRole("button", { name: "Run the demo" }).click();
  await expect(
    page.getByRole("button", { name: "Approve request" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: "Waiting for manager approval" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Approve request" }).click();
  await expect(page.getByRole("status")).toContainText("Workflow complete");
  await page
    .getByRole("button", { name: "Document handoff", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Run the demo" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Run the demo" }).click();
  await page
    .getByRole("button", { name: "Simulate callback", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText("Workflow complete");
  await page.getByRole("button", { name: "Reset", exact: true }).click();
  await expect(page.getByRole("status")).toContainText("A little structure");
});

test("JSON follows the selected example and scenario changes cancel pending animation", async ({
  page,
}) => {
  await page.goto("/#playground");
  await page.getByRole("button", { name: "Run the demo" }).click();
  await page
    .getByRole("button", { name: "AI-assisted review", exact: true })
    .click();
  await page.waitForTimeout(1600);
  await expect(page.getByRole("status")).toContainText("A little structure");
  await page.getByRole("button", { name: "View JSON" }).click();
  const definition = JSON.parse(
    await page.locator(".json-panel code").innerText(),
  );
  expect(definition.id).toBe("ai-review");
  expect(definition.steps[0].type).toBe("llm");
  expect(definition.version).toBe(0);
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy JSON" }).click();
  await expect(page.locator(".copy-message")).toHaveText("JSON copied");
  expect(
    JSON.parse(await page.evaluate(() => navigator.clipboard.readText())),
  ).toEqual(definition);
  await page.getByRole("button", { name: "View canvas" }).click();
  await page.getByRole("button", { name: /Generate a draft AI/ }).click();
  await expect(page.locator(".node-explanation")).toContainText(
    "does not call a model",
  );
});

test("preview rejects unknown resources and writes", async ({ request }) => {
  expect((await request.get("/missing-page")).status()).toBe(404);
  expect((await request.get("/%2e%2e%2fpackage.json")).status()).toBe(404);
  expect((await request.post("/")).status()).toBe(405);
});
