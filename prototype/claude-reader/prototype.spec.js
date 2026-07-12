const { test, expect } = require("@playwright/test");

test.use({ viewport: { width: 1440, height: 900 } });

test("prototype interactions remain coherent", async ({ page }) => {
  await page.goto("http://127.0.0.1:8792/");
  await expect(page.locator("#article-title")).toContainText("编码评测");

  await page.locator('[data-article-id="context"]').click();
  await expect(page.locator("#article-title")).toContainText("Passation");

  await page.locator('[data-view="recommended"]').click();
  await expect(page.locator(".article-row")).toHaveCount(3);

  await page.locator("#drawer-open").click();
  await expect(page.locator("#library-drawer")).toHaveClass(/is-open/);
  await page.waitForTimeout(250);
  await page.screenshot({ path: "desktop-1440-drawer.png" });
  await page.locator("#drawer-close").click();

  await page.locator("#feed-button").click();
  await expect(page.locator("#feed-dialog")).toBeVisible();
  await page.getByRole("button", { name: "取消", exact: true }).click();

  await page.locator("#run-button").click();
  await expect(page.locator("#run-state")).toContainText("分析完成", { timeout: 5000 });
  await expect(page.locator("#run-button")).toBeEnabled();
});
