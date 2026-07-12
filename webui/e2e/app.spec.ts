import { expect, test } from "@playwright/test";

test("loads real digest and supports reader workflow", async ({ page }, testInfo) => {
  await page.goto("/");
  await expect(page.locator(".article-row").first()).toBeVisible();
  await expect(page.locator("#reader h2")).not.toBeEmpty();

  const firstTitle = await page.locator("#reader h2").textContent();
  await page.locator(".article-row").nth(1).click();
  await expect(page.locator("#reader h2")).not.toHaveText(firstTitle || "");

  await page.getByRole("tab", { name: "推荐", exact: true }).click();
  await expect(page.locator(".article-row").first()).toBeVisible();

  await page.getByRole("button", { name: "打开来源与历史" }).click();
  await expect(page.locator(".library-drawer")).toHaveClass(/is-open/);
  await expect(page.getByText("Reddit · Claude Engineering", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "关闭来源与历史" }).click();
  await page.waitForTimeout(250);

  await page.keyboard.press("Control+k");
  await expect(page.locator(".command-dialog")).toBeVisible();
  await page.keyboard.press("Escape");

  const width = await page.evaluate(() => ({ scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth }));
  expect(width.scroll).toBeLessThanOrEqual(width.client);
  await page.screenshot({ path: `../prototype/claude-reader/react-${testInfo.project.name}.png`, fullPage: false });
});

test("run and feedback controls use existing API contract", async ({ page }) => {
  let runCalled = false;
  let feedbackCalled = false;
  await page.route("**/api/run?**", async (route) => {
    runCalled = true;
    await route.fulfill({ json: { run_id: 99, profile: "default", fetched: 12, candidate: 3, analyzed: 3, pushed: 0, cached: 0, queued: 0, rate_limited: 0, errors: [] } });
  });
  await page.route("**/api/feedback", async (route) => {
    if (route.request().method() === "POST") feedbackCalled = true;
    await route.fulfill({ json: { profile: "default", item_id: "mock", action: "like" } });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "立即运行" }).click();
  await expect.poll(() => runCalled).toBeTruthy();
  await page.getByRole("button", { name: "有用" }).click();
  await expect.poll(() => feedbackCalled).toBeTruthy();
});

test("scrolling loads the next database page", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "desktop", "Pagination is covered once at desktop size");
  await page.goto("/");
  const initial = await page.locator(".article-row").count();
  await page.locator(".article-list").evaluate((element) => { element.scrollTop = element.scrollHeight; element.dispatchEvent(new Event("scroll")); });
  await expect.poll(() => page.locator(".article-row").count()).toBeGreaterThan(initial);
});
