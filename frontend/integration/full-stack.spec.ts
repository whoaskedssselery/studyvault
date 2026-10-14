import { test, expect } from "@playwright/test";
const base = "http://127.0.0.1:5175";

test("real stack: guest, registration, verification mail, PDF upload, publication, moderation and account cleanup", async ({
  page,
  browser,
}) => {
  const email = `frontend-${Date.now()}@example.test`,
    password = "frontend-integration-password",
    title = `Конспект браузерной проверки ${Date.now()}`;
  await page.goto("/");
  await expect(page.locator(".live-material-card").first()).toBeVisible();
  await page.goto("/register");
  await page.getByLabel("Имя и фамилия").fill("Студент браузерной проверки");
  await page.getByLabel("Электронная почта").fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill(password);
  await page
    .getByRole("button", { name: "Создать аккаунт", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "Подтверждение почты" }),
  ).toBeVisible();
  let confirmation = "";
  await expect
    .poll(
      async () => {
        const search = await page.request.get(
          `http://localhost:8025/api/v1/search?query=${encodeURIComponent(`to:${email}`)}`,
        );
        const list = await search.json();
        for (const entry of list.messages || []) {
          const response = await page.request.get(
            `http://localhost:8025/api/v1/message/${entry.ID}`,
          );
          const message = await response.json();
          const link = String(message.Text || "").match(
            /http:\/\/127\.0\.0\.1:5175\/verify-email\?token=[A-Za-z0-9_-]+/,
          );
          if (link) {
            confirmation = link[0];
            return true;
          }
        }
        return false;
      },
      { timeout: 20000 },
    )
    .toBe(true);
  // Keep a navigation failure from printing the one-time token in a call log.
  await page.goto(confirmation).catch(() => {
    throw new Error("The verification page could not be opened");
  });
  await expect(
    page.getByRole("heading", { name: "Почта подтверждена", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => new URL(location.href).searchParams.has("token")),
  ).toBe(false);
  await page.getByRole("button", { name: "В библиотеку", exact: true }).click();
  await page
    .getByRole("button", { name: "Добавить материал", exact: true })
    .click();
  await page.getByLabel("Название материала").fill(title);
  await page
    .getByLabel("Краткое описание")
    .fill("Проверка фронтенда на настоящем сервере");
  await expect
    .poll(() =>
      page
        .getByLabel("Общий предмет", { exact: true })
        .locator("option")
        .count(),
    )
    .toBeGreaterThan(1);
  await page
    .getByLabel("Общий предмет", { exact: true })
    .selectOption({ index: 1 });
  await page
    .getByLabel("Текст материала", { exact: true })
    .fill(
      "Текст сохраняется в PostgreSQL и становится доступен после решения модератора.",
    );
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page).toHaveURL(/\/edit\/[0-9a-f-]+$/);
  const id = new URL(page.url()).pathname.split("/").at(-1)!;
  const cors = await page.request.fetch("http://localhost:9000/", {
    method: "OPTIONS",
    headers: {
      Origin: base,
      "Access-Control-Request-Method": "PUT",
      "Access-Control-Request-Headers": "content-type",
    },
  });
  expect(cors.headers()["access-control-allow-origin"]).toBe(base);
  let uploaded = false,
    uploadHasBearer = false;
  page.on("response", (response) => {
    if (
      response.request().method() === "PUT" &&
      new URL(response.url()).port === "9000"
    ) {
      uploaded = response.ok();
      uploadHasBearer = !!response.request().headers().authorization;
    }
  });
  await page.getByLabel("Прикрепить файл", { exact: true }).setInputFiles({
    name: "frontend-smoke.pdf",
    mimeType: "application/pdf",
    buffer: Buffer.from("%PDF-1.4\n%%EOF\n"),
  });
  await expect.poll(() => uploaded).toBe(true);
  expect(uploadHasBearer).toBe(false);
  await expect(page.locator(".api-files .attachment")).toContainText(
    "frontend-smoke.pdf",
  );
  await page
    .getByRole("button", { name: "Отправить на проверку", exact: true })
    .click();
  await expect(page).toHaveURL(/\/my$/);
  await expect(page.getByText("На проверке", { exact: true })).toBeVisible();
  const moderator = await browser.newPage({ baseURL: base });
  const guest = await browser.newPage({ baseURL: base });
  try {
    await moderator.goto("/login");
    await moderator
      .getByLabel("Электронная почта")
      .fill("moderator@example.com");
    await moderator
      .getByLabel("Пароль", { exact: true })
      .fill("demo-password-123");
    await moderator.getByRole("button", { name: "Войти", exact: true }).click();
    await moderator
      .getByRole("navigation", { name: "Основная навигация" })
      .getByRole("button", { name: "Модерация", exact: true })
      .click();
    const row = moderator
      .locator(".moderation-card")
      .filter({ hasText: title });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Одобрить", exact: true }).click();
    await expect(row).toHaveCount(0);
    await guest.goto(`/notes/${id}`);
    await expect(
      guest.getByRole("heading", { name: title, exact: true }),
    ).toBeVisible();
    await expect(
      guest.getByText(
        "Текст сохраняется в PostgreSQL и становится доступен после решения модератора.",
        { exact: true },
      ),
    ).toBeVisible();
    const downloaded = guest.waitForResponse((response) =>
      new URL(response.url()).pathname.startsWith(
        `/v1/library/${id}/attachments/`,
      ),
    );
    await guest.getByRole("button", { name: /frontend-smoke.pdf/ }).click();
    const link = await (await downloaded).json();
    const file = await guest.request.get(link.download_url).catch(() => {
      throw new Error("The uploaded PDF could not be downloaded");
    });
    expect(file.status()).toBe(200);
    expect((await file.body()).length).toBe(15);
    await guest.screenshot({
      path: "test-results/real-stack-material.png",
      fullPage: true,
    });
    await page.goto("/profile");
    await page
      .getByRole("button", { name: "Удалить аккаунт", exact: true })
      .click();
    await page.getByLabel("Пароль для удаления").fill(password);
    await page
      .getByRole("button", { name: "Удалить навсегда", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Войти", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(() =>
        sessionStorage.getItem("studyvault.api.user.v1"),
      ),
    ).toBeNull();
  } finally {
    await moderator.close();
    await guest.close();
  }
});
