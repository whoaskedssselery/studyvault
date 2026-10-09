import { test, expect } from "@playwright/test";
import path from "node:path";

test("search, filters and favorites persist after reload", async ({ page }) => {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Библиотека знаний." }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "Поиск материалов" }).fill("SQL");
  await expect(page.locator(".material-card")).toHaveCount(2);
  await page
    .getByRole("textbox", { name: "Поиск материалов" })
    .fill("ничего-не-найдется");
  await expect(
    page.getByRole("heading", { name: "Пока ничего не нашлось" }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "Поиск материалов" }).fill("");
  await page
    .getByRole("button", {
      name: "Добавить в избранное: SQL без паники: JOIN, GROUP BY и подзапросы",
      exact: true,
    })
    .click();
  await page.reload();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Избранное" })
    .click();
  await expect(
    page.getByRole("button", {
      name: "SQL без паники: JOIN, GROUP BY и подзапросы",
      exact: true,
    }),
  ).toBeVisible();
});

test("create, moderate, comment and rate a note", async ({ page }) => {
  await page.goto("/new");
  await page
    .getByLabel("Название материала")
    .fill("Тестовый конспект по интегралам");
  await page
    .getByLabel("Краткое описание")
    .fill("Понятный конспект для подготовки к экзамену.");
  await page
    .getByRole("textbox", { name: "Текст материала" })
    .fill(
      "Интеграл помогает вычислять площади. Это проверка публикации и хранения текста.",
    );
  await page.getByLabel("Теги").fill("интегралы, матан");
  await page
    .getByRole("button", { name: "Отправить на проверку", exact: true })
    .click();
  await expect(page).toHaveURL(/\/my$/);
  await page.goto("/moderation");
  const item = page
    .locator(".moderation-card")
    .filter({ hasText: "Тестовый конспект по интегралам" });
  await item.getByRole("button", { name: "Одобрить", exact: true }).click();
  await expect(item).toHaveCount(0);
  await page.goto("/");
  await page
    .getByRole("textbox", { name: "Поиск материалов" })
    .fill("Тестовый конспект");
  await page
    .getByRole("button", {
      name: "Тестовый конспект по интегралам",
      exact: true,
    })
    .click();
  await expect(
    page.getByText(
      "Интеграл помогает вычислять площади. Это проверка публикации и хранения текста.",
      { exact: true },
    ),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Комментарий" })
    .fill("Спасибо! Теперь всё понятно.");
  await page.getByRole("button", { name: "Отправить комментарий" }).click();
  await page.getByRole("button", { name: "Оценить на 5", exact: true }).click();
  await page.reload();
  await expect(
    page.getByText("Спасибо! Теперь всё понятно.", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".big-rating strong")).toHaveText("5.0");
});

test("registration, logout, demo login and access guard", async ({ page }) => {
  await page.goto("/register");
  await page.getByLabel("Имя и фамилия").fill("Новый Студент");
  await page.getByLabel("Электронная почта").fill("new@example.test");
  await page.getByLabel("Демонстрационный пароль").fill("demo-only-password");
  await page.getByRole("button", { name: "Создать демоаккаунт" }).click();
  await expect(page).toHaveURL(/\/$/);
  await page.goto("/moderation");
  await expect(
    page.getByRole("heading", { name: "Раздел модератора" }),
  ).toBeVisible();
  await page.goto("/profile");
  await expect(
    page.getByRole("heading", { name: "Новый Студент" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Выйти", exact: true }).click();
  await page.getByRole("button", { name: "Войти в демо", exact: true }).click();
  await page.goto("/moderation");
  await expect(
    page.getByRole("heading", { name: "Модерация материалов." }),
  ).toBeVisible();
});

test("draft can be reopened and edited; rejection has a reason", async ({
  page,
}) => {
  await page.goto("/new");
  await page.getByLabel("Название материала").fill("Мой черновик");
  await page
    .getByRole("textbox", { name: "Текст материала" })
    .fill("Текст черновика, который должен сохраниться.");
  await page
    .getByRole("button", { name: "Сохранить черновик", exact: true })
    .click();
  const id = await page.evaluate(
    () =>
      JSON.parse(localStorage.getItem("studyvault.database.v1")!).notes.find(
        (n: { title: string }) => n.title === "Мой черновик",
      ).id,
  );
  await page.goto(`/edit/${id}`);
  await expect(page.getByLabel("Название материала")).toHaveValue(
    "Мой черновик",
  );
  await page.getByLabel("Название материала").fill("Исправленный черновик");
  await page
    .getByRole("button", { name: "Отправить на проверку", exact: true })
    .click();
  await page.goto("/moderation");
  await page
    .locator(".moderation-card")
    .filter({ hasText: "Исправленный черновик" })
    .getByRole("button", { name: "Отклонить", exact: true })
    .click();
  await page
    .getByRole("textbox", { name: "Причина отклонения" })
    .fill("Добавьте источники");
  await page
    .getByRole("button", { name: "Отклонить материал", exact: true })
    .click();
  await page.reload();
  await page.getByRole("button", { name: /Отклонён/ }).click();
  await expect(page.getByText("Причина: Добавьте источники")).toBeVisible();
});

test("keyboard search keeps focus and uploaded files persist", async ({
  page,
}) => {
  await page.goto("/");
  await page.keyboard.press("Control+k");
  const search = page.getByRole("textbox", { name: "Быстрый поиск" });
  await expect(search).toBeFocused();
  await page.keyboard.type("SQL");
  await expect(search).toHaveValue("SQL");
  await expect(search).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.goto("/new");
  await page.getByLabel("Название материала").fill("Конспект с вложением");
  await page
    .getByRole("textbox", { name: "Текст материала" })
    .fill("Проверка сохранения вложенного файла.");
  await page
    .locator("input[type=file]")
    .setInputFiles({
      name: "example.pdf",
      mimeType: "application/pdf",
      buffer: Buffer.from("%PDF-1.4\n%%EOF"),
    });
  await expect(page.getByText("example.pdf", { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: "Сохранить черновик", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Конспект с вложением", exact: true })
    .click();
  await page.reload();
  await expect(page.getByRole("link", { name: /example.pdf/ })).toHaveAttribute(
    "download",
    "example.pdf",
  );
});

test("desktop and mobile screenshots without runtime errors", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const folder = path.resolve("..", "docs", "screenshots");
  for (const [url, name] of [
    ["/", "01-library"],
    ["/notes/note-1", "02-note"],
    ["/new", "03-editor"],
    ["/ranking", "04-ranking"],
    ["/moderation", "05-moderation"],
    ["/login", "06-login"],
    ["/register", "07-register"],
    ["/profile", "08-profile"],
  ]) {
    await page.goto(url);
    await expect(page.locator("h1").first()).toBeVisible();
    if (url === "/")
      await page.screenshot({
        path: path.join(folder, "00-library-overview.png"),
      });
    if (url === "/new") {
      await page
        .getByLabel("Название материала")
        .fill("Интегралы простыми словами");
      await page
        .getByLabel("Краткое описание")
        .fill(
          "Определения, основные формулы и примеры для подготовки к семинару.",
        );
      await page
        .getByRole("textbox", { name: "Текст материала" })
        .fill(
          "Что такое интеграл?\nИнтеграл позволяет найти площадь под графиком функции.\n\nОсновные правила\nДля степенной функции: ∫xⁿ dx = xⁿ⁺¹ / (n + 1) + C, где n ≠ −1.\n\nРазбираем пример\n∫2x dx = x² + C. Проверьте результат, взяв производную.\n\nПроверьте себя\nВычислите ∫3x² dx и объясните, почему в ответе появляется константа C.",
        );
      await page.getByLabel("Теги").fill("интегралы, матан, 2 семестр");
    }
    await page.screenshot({
      path: path.join(folder, `${name}.png`),
      fullPage: true,
    });
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Библиотека знаний." }),
  ).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Открыть меню", exact: true }).click();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Избранное" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Всегда под рукой." }),
  ).toBeVisible();
  await page.goto("/");
  await page.screenshot({
    path: path.join(folder, "09-mobile-library.png"),
    fullPage: true,
  });
  expect(errors).toEqual([]);
});
