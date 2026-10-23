import { test, expect } from "@playwright/test";
import { ids, liveFixture, liveLogin } from "./live-fixture";
test.use({ storageState: { cookies: [], origins: [] } });

test("quick search works from a material, supports the keyboard and opens a real result", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await page.goto(`/notes/${ids.material}`);
  await expect(
    page.getByRole("heading", { name: "Соединяем таблицы" }),
  ).toBeVisible();
  await page.keyboard.press("Control+k");
  const dialog = page.getByRole("dialog", { name: "Поиск по библиотеке" });
  const search = page.getByRole("combobox", {
    name: "Быстрый поиск материалов",
  });
  await expect(search).toBeFocused();
  await search.fill("SQL");
  await expect
    .poll(() =>
      data.calls.some(
        (call) =>
          call.path === "/library" &&
          new URLSearchParams(call.query).get("q") === "SQL",
      ),
    )
    .toBe(true);
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await search.press("Enter");
  await expect(dialog).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`/notes/${ids.material}$`));
  await page.getByRole("button", { name: /Быстрый поиск/ }).click();
  await expect(search).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /Быстрый поиск/ }),
  ).toBeFocused();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe("");
});

test("subjects and help open complete panels, including on mobile and in dark mode", async ({
  page,
}) => {
  await liveFixture(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Все предметы", exact: true }).click();
  const subjects = page.getByRole("dialog", { name: "Предметы библиотеки" });
  await subjects.getByRole("button", { name: /Базы данных/ }).click();
  await expect(subjects).toHaveCount(0);
  await expect(page.getByLabel("Предмет библиотеки")).toHaveValue(ids.subject);
  await page.getByRole("button", { name: "Тёмная тема", exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Открыть меню", exact: true }).click();
  await page.getByRole("button", { name: "О проекте", exact: true }).click();
  const help = page.getByRole("dialog", {
    name: "Как пользоваться StudyVault",
  });
  await expect(
    help.getByRole("heading", { name: "Делитесь знаниями" }),
  ).toBeVisible();
  await expect(help).toContainText("До трёх файлов по 2 МБ");
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.keyboard.press("Escape");
  await expect(help).toHaveCount(0);
});

test("the activity drawer reads the user's note statuses and links to editing", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  data.publishOwn();
  await page.getByRole("button", { name: "Уведомления", exact: true }).click();
  const activity = page.getByRole("dialog", { name: "Активность материалов" });
  await expect(
    activity.getByText("Опубликован", { exact: true }),
  ).toBeVisible();
  await activity.getByRole("button", { name: /Мой черновик/ }).click();
  await expect(activity).toHaveCount(0);
  await expect(page).toHaveURL(new RegExp(`/edit/${ids.own}$`));
});

test("guest activity offers login without making private API requests", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Уведомления", exact: true }).click();
  const activity = page.getByRole("dialog", { name: "Активность материалов" });
  await expect(
    activity.getByRole("heading", { name: "Будьте в курсе публикаций" }),
  ).toBeVisible();
  expect(data.calls.some((call) => call.path === "/notes")).toBe(false);
  await activity.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("leaving an edited note requires a choice and staying preserves all edits", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  await page.goto(`/edit/${ids.own}`);
  await expect(page.getByLabel("Название материала")).toHaveValue(
    "Мой черновик",
  );
  await page.getByLabel("Название материала").fill("Не потеряйте мой текст");
  await page
    .getByLabel("Текст материала", { exact: true })
    .fill("Несохранённый конспект");
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Библиотека", exact: true })
    .click();
  const confirm = page.getByRole("dialog", { name: "Несохранённые изменения" });
  await expect(confirm).toBeVisible();
  await confirm.getByRole("button", { name: "Остаться в редакторе" }).click();
  await expect(page.getByLabel("Название материала")).toHaveValue(
    "Не потеряйте мой текст",
  );
  await expect(
    page.getByLabel("Текст материала", { exact: true }),
  ).toContainText("Несохранённый конспект");
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Библиотека", exact: true })
    .click();
  await confirm.getByRole("button", { name: "Уйти без сохранения" }).click();
  await expect(page).toHaveURL("http://127.0.0.1:5173/");
  expect(
    data.calls.some(
      (call) => call.path === `/notes/${ids.own}` && call.method === "PATCH",
    ),
  ).toBe(false);
});

test("saving clears the leave warning while a failed save keeps it", async ({
  page,
}) => {
  await liveFixture(page);
  await liveLogin(page);
  await page.goto(`/edit/${ids.own}`);
  await page.getByLabel("Название материала").fill("Сохранённый конспект");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(
    page.getByText("Все изменения сохранены", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Мои материалы", exact: true })
    .click();
  await expect(page).toHaveURL(/\/my$/);
  await page
    .getByRole("button", { name: "Редактировать", exact: true })
    .click();
  await page
    .getByLabel("Название материала")
    .fill("Изменение при сбое сервера");
  await page.route(`**/notes/${ids.own}`, (route) =>
    route.request().method() === "PATCH"
      ? route.fulfill({
          status: 503,
          contentType: "application/problem+json",
          json: {
            status: 503,
            code: "unavailable",
            detail: "Попробуйте позже",
          },
        })
      : route.fallback(),
  );
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Библиотека", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Несохранённые изменения" }),
  ).toBeVisible();
});
