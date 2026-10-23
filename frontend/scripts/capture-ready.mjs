import { chromium, expect } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";
const baseURL = process.env.STUDYVAULT_BASE_URL || "http://localhost:5173";
const password = process.env.SEED_PASSWORD || "demo-password-123";
const directory = resolve("../docs/submission/screenshots");
await mkdir(directory, { recursive: true });
const browser = await chromium.launch();
const errors = [];
const shots = [];
let initialJsBytes = 0;
const create = async () => {
  const context = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 1000 },
  });
  const page = await context.newPage();
  page.on("pageerror", (error) => errors.push(error.message));
  return page;
};
const shot = async (page, name, description) => {
  await page.screenshot({
    path: resolve(directory, name + ".png"),
    fullPage: ["08-editor", "10-profile", "11-moderation"].includes(name),
  });
  shots.push({ file: name + ".png", description });
};
const signIn = async (page, email) => {
  await page.goto("/login");
  await page.getByLabel("Электронная почта").fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Меню пользователя" }),
  ).toBeVisible();
};
try {
  const guest = await create();
  await guest.goto("/?mode=api");
  await expect(guest.locator(".live-material-card").first()).toBeVisible();
  initialJsBytes = await guest.evaluate(() =>
    performance
      .getEntriesByType("resource")
      .filter((entry) => new URL(entry.name).pathname.endsWith(".js"))
      .reduce((total, entry) => total + entry.decodedBodySize, 0),
  );
  await shot(
    guest,
    "01-library",
    "Гостевая библиотека с материалами из PostgreSQL",
  );
  await guest.locator(".live-material-card .card-title").first().click();
  await expect(guest.locator(".reader h1")).toBeVisible();
  await shot(
    guest,
    "02-material",
    "Опубликованный материал, оценки, вложения и обсуждение",
  );
  await guest.keyboard.press("Control+k");
  await expect(
    guest.getByRole("combobox", { name: "Быстрый поиск материалов" }),
  ).toBeFocused();
  await expect(guest.getByRole("option").first()).toBeVisible();
  await shot(guest, "03-search", "Быстрый поиск по настоящей библиотеке");
  await guest.keyboard.press("Escape");
  await guest
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Рейтинг авторов" })
    .click();
  await expect(guest.locator("tbody tr").first()).toBeVisible();
  await shot(guest, "04-ranking", "Рейтинг авторов по серверным оценкам");
  await guest
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Библиотека", exact: true })
    .click();
  await expect(guest.locator(".live-material-card").first()).toBeVisible();
  await guest.getByRole("button", { name: "Тёмная тема", exact: true }).click();
  await shot(guest, "05-dark-library", "Тёмная тема библиотеки");
  await guest.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      guest.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    )
    .toBe(true);
  await shot(guest, "06-mobile-dark", "Мобильная версия в тёмной теме");

  const student = await create();
  await student.goto("/login");
  await expect(student.getByLabel("Электронная почта")).toBeVisible();
  await shot(student, "07-login", "Вход в аккаунт");
  await signIn(student, "demo@example.com");
  await student
    .getByRole("button", { name: "Добавить материал", exact: true })
    .click();
  await student
    .getByLabel("Название материала")
    .fill("Индексы PostgreSQL: коротко и по делу");
  await student
    .getByLabel("Краткое описание")
    .fill(
      "Как индекс ускоряет поиск и почему его не стоит создавать на каждом столбце.",
    );
  const subject = student.getByLabel("Общий предмет", { exact: true });
  await expect.poll(() => subject.locator("option").count()).toBeGreaterThan(1);
  await subject.selectOption({ label: "Базы данных" });
  await student
    .getByLabel("Текст материала", { exact: true })
    .fill(
      "Индекс — дополнительная структура данных для быстрого поиска строк.\n\nПроверьте план запроса через EXPLAIN ANALYZE. Индекс особенно полезен для фильтрации и соединения больших таблиц.",
    );
  await student
    .getByLabel("Теги", { exact: true })
    .fill("PostgreSQL, SQL, индексы");
  await shot(
    student,
    "08-editor",
    "Редактор с заполненным, ещё не сохранённым конспектом",
  );
  await student
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Мои материалы", exact: true })
    .click();
  await expect(
    student.getByRole("dialog", { name: "Несохранённые изменения" }),
  ).toBeVisible();
  await shot(student, "09-unsaved", "Защита текста при уходе из редактора");
  await student.getByRole("button", { name: "Уйти без сохранения" }).click();
  await student.getByRole("button", { name: "Меню пользователя" }).click();
  await student.getByRole("menuitem", { name: "Мой профиль" }).click();
  await expect(student.getByLabel("Имя и фамилия")).toBeVisible();
  await shot(student, "10-profile", "Настройки профиля и аккаунта");

  const moderator = await create();
  await signIn(moderator, "moderator@example.com");
  await moderator
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Модерация", exact: true })
    .click();
  await expect(
    moderator.getByRole("heading", { name: "Общие предметы", exact: true }),
  ).toBeVisible();
  await shot(
    moderator,
    "11-moderation",
    "Очередь модерации и управление общими предметами",
  );
  if (errors.length) throw new Error(`Browser errors: ${errors.length}`);
  await writeFile(
    resolve(directory, "../capture.json"),
    JSON.stringify(
      {
        capturedAt: new Date().toISOString(),
        sourceCommit: execFileSync("git", ["rev-parse", "HEAD"], {
          encoding: "utf8",
        }).trim(),
        initialJsBytes,
        baseURL,
        source:
          "real local API, PostgreSQL, Meilisearch and RustFS; no intercepted responses",
        screenshots: shots,
        browserErrors: errors.length,
      },
      null,
      2,
    ),
  );
  console.log(
    `Captured ${shots.length} screenshots from the real local stack; no browser errors.`,
  );
} finally {
  await browser.close();
}
