import { test, expect } from "@playwright/test";
import { ids, liveFixture } from "./live-fixture";
test.use({ storageState: { cookies: [], origins: [] } });

test("production build uses /v1 and preserves the connected interface on mobile", async ({
  page,
}) => {
  const data = await liveFixture(page);
  const paths: string[] = [];
  page.on("request", (request) => paths.push(new URL(request.url()).pathname));
  await page.goto("http://127.0.0.1:5175/");
  await expect(
    page.getByRole("heading", { name: "Библиотека знаний." }),
  ).toBeVisible();
  await expect(page.locator(".live-material-card")).toHaveCount(1);
  expect(paths).toContain("/v1/library");
  expect(paths.some((path) => path.startsWith("/backend-api"))).toBe(false);
  await page.screenshot({
    path: "../docs/assignment-2/screenshots/12-connected-library.png",
    fullPage: true,
  });
  await page
    .getByRole("button", { name: "SQL: понятные соединения", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Соединяем таблицы" }),
  ).toBeVisible();
  await page.screenshot({
    path: "../docs/assignment-2/screenshots/13-connected-material.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("http://127.0.0.1:5175/");
  await expect(
    page.getByRole("heading", { name: "Библиотека знаний." }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await page.screenshot({
    path: "../docs/assignment-2/screenshots/14-connected-mobile.png",
  });
  expect(
    data.calls.some((call) => call.path === `/library/${ids.material}`),
  ).toBe(true);
});

test("portable HTML can explicitly use the same API with hash routes and its proxy", async ({
  page,
}) => {
  await liveFixture(page);
  await page.goto("http://127.0.0.1:4174/?mode=api#/");
  await expect(page.locator(".live-material-card")).toHaveCount(1);
  await page
    .getByRole("button", { name: "SQL: понятные соединения", exact: true })
    .click();
  await expect(page).toHaveURL(/#\/notes\//);
  await page.reload();
  await expect(
    page.getByRole("heading", {
      name: "SQL: понятные соединения",
      exact: true,
    }),
  ).toBeVisible();
});
