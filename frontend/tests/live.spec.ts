import { test, expect } from "@playwright/test";
import { ids, liveFixture, liveLogin } from "./live-fixture";
test.use({ storageState: { cookies: [], origins: [] } });

test("guests use the real library, filters, safe search, cursor, rating and author profiles", async ({
  page,
}) => {
  const data = await liveFixture(page);
  const mocks: string[] = [];
  page.on("request", (request) => {
    if (request.url().endsWith("/mock.json")) mocks.push(request.url());
  });
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Библиотека знаний." }),
  ).toBeVisible();
  await expect(
    page.getByText("Нет оценок", { exact: true }).first(),
  ).toBeVisible();
  await page.getByLabel("Поиск материалов").fill("SQL");
  await expect(page.locator(".material-card mark")).toHaveText("SQL");
  expect(await page.evaluate(() => (window as any).bad)).toBeUndefined();
  await page.getByLabel("Предмет библиотеки").selectOption(ids.subject);
  await page
    .getByLabel("Тип материала", { exact: true })
    .selectOption("cheat_sheet");
  await page.getByLabel("Семестр", { exact: true }).selectOption("2");
  await page.getByLabel("Фильтр по тегу").fill("SQL");
  await page.getByLabel("Сортировка").selectOption("views");
  await expect
    .poll(() =>
      data.calls.some(
        (call) =>
          call.path === "/library" &&
          new URLSearchParams(call.query).get("tag") === "SQL",
      ),
    )
    .toBe(true);
  const query = new URLSearchParams(
    data.calls.filter((call) => call.path === "/library").at(-1)!.query,
  );
  expect(query.get("library_subject_id")).toBe(ids.subject);
  expect(query.get("kind")).toBe("cheat_sheet");
  expect(query.get("semester")).toBe("2");
  expect(query.get("sort")).toBe("views");
  await expect(
    page.getByRole("button", { name: "Показать ещё", exact: true }),
  ).toHaveCount(0);
  await page.getByLabel("Поиск материалов").fill("");
  await page.getByRole("button", { name: "Показать ещё", exact: true }).click();
  await expect
    .poll(() =>
      data.calls.some(
        (call) =>
          new URLSearchParams(call.query).get("cursor") === "library-next",
      ),
    )
    .toBe(true);
  await page
    .getByRole("button", { name: "SQL: понятные соединения", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Соединяем таблицы" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Оценить на 5", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Профиль автора →" }).click();
  await expect(
    page.getByRole("heading", { name: "Максим Волков" }),
  ).toBeVisible();
  expect(mocks).toEqual([]);
  expect(
    data.calls
      .filter(
        (call) => call.path.startsWith("/library") && call.method === "GET",
      )
      .every((call) => !call.auth),
  ).toBe(true);
});

test("verified students rate, bookmark, comment and edit their own comments", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  await page
    .getByRole("button", { name: "SQL: понятные соединения", exact: true })
    .click();
  await page.getByRole("button", { name: "Оценить на 5", exact: true }).click();
  await expect(page.locator(".big-rating strong")).toHaveText("5.0");
  await page
    .getByRole("button", { name: "Сохранить материал", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Убрать из избранного", exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Комментарий", { exact: true })
    .fill("Спасибо за понятный конспект");
  await page
    .getByRole("button", { name: "Отправить комментарий", exact: true })
    .click();
  await expect(page.locator(".comment")).toContainText(
    "Спасибо за понятный конспект",
  );
  await page
    .locator(".comment")
    .getByRole("button", { name: "Изменить", exact: true })
    .click();
  await page.getByLabel("Изменить комментарий").fill("Теперь всё понятно!");
  await page.getByRole("button", { name: "Сохранить комментарий" }).click();
  await expect(page.locator(".comment")).toContainText("изменён");
  page.once("dialog", (dialog) => dialog.accept());
  await page
    .getByRole("button", { name: "Удалить комментарий", exact: true })
    .click();
  await expect(page.locator(".comment")).toHaveCount(0);
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Избранное", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "SQL: понятные соединения", exact: true }),
  ).toBeVisible();
  expect(
    data.calls.find(
      (call) => call.path.endsWith("/rating") && call.method === "PUT",
    )?.body,
  ).toEqual({ value: 5 });
});

test("unverified students see verification and cannot publish or participate; resend obeys Retry-After", async ({
  page,
}) => {
  const data = await liveFixture(page, { verified: false, resend429: true });
  await liveLogin(page);
  await expect(
    page.getByRole("region", { name: "Подтверждение почты" }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Отправить ещё раз", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: /Повторить через/ }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Отправить ещё раз", exact: true }),
  ).toBeEnabled({ timeout: 5000 });
  await page
    .getByRole("button", { name: "SQL: понятные соединения", exact: true })
    .click();
  await expect(page.getByLabel("Комментарий", { exact: true })).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Оценить на 5", exact: true }),
  ).toBeDisabled();
  await page
    .getByRole("button", { name: "Сохранить материал", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Убрать из избранного", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Мои материалы", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Новый материал", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Отправить на проверку", exact: true }),
  ).toBeDisabled();
  expect(
    data.calls.some(
      (call) => call.path.endsWith("/rating") || call.path.endsWith("/submit"),
    ),
  ).toBe(false);
});

test("creation saves Tiptap metadata and tags, then submits; pending can return to draft", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  await page
    .getByRole("button", { name: "Добавить материал", exact: true })
    .click();
  await page.getByLabel("Название материала").fill("Индексы PostgreSQL");
  await page.getByLabel("Краткое описание").fill("Когда индекс помогает");
  await page
    .getByLabel("Общий предмет", { exact: true })
    .selectOption(ids.subject);
  await page
    .getByLabel("Текст материала", { exact: true })
    .fill("Индекс ускоряет поиск строк в таблице.");
  await page.getByLabel("Теги", { exact: true }).fill("SQL");
  await page
    .getByRole("button", { name: "Отправить на проверку", exact: true })
    .click();
  await expect(page).toHaveURL(/\/my$/);
  await expect(page.getByText("На проверке", { exact: true })).toBeVisible();
  const create = data.calls.find(
    (call) => call.path === "/notes" && call.method === "POST",
  );
  expect(create?.body.library_subject_id).toBe(ids.subject);
  expect(create?.body.content.type).toBe("doc");
  expect(create?.body.kind).toBe("note");
  expect(data.calls.some((call) => call.path.endsWith("/submit"))).toBe(true);
  await page
    .getByRole("button", { name: "Редактировать", exact: true })
    .click();
  await page.getByRole("button", { name: "Забрать в черновики" }).click();
  await expect(page.getByText("Черновик", { exact: true })).toBeVisible();
});

test("moderators review a real queue and require reasons; shared subject management uses its own endpoints", async ({
  page,
}) => {
  const data = await liveFixture(page, { role: "moderator" });
  await liveLogin(page);
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Модерация", exact: true })
    .click();
  await page.getByRole("button", { name: "Отклонить", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Отклонить материал", exact: true }),
  ).toBeDisabled();
  await page
    .getByLabel("Причина отклонения", { exact: true })
    .fill("Укажите источник");
  await page
    .getByRole("button", { name: "Отклонить материал", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Все материалы проверены" }),
  ).toBeVisible();
  await page.getByLabel("Новый общий предмет").fill("Физика");
  await page
    .getByRole("button", { name: "Создать предмет", exact: true })
    .click();
  await expect(
    page.locator(".live-subject-row").filter({ hasText: "Физика" }),
  ).toBeVisible();
  const row = page.locator(".live-subject-row").filter({ hasText: "Физика" });
  await row.getByRole("button", { name: "Изменить предмет" }).click();
  await page
    .getByLabel("Название общего предмета", { exact: true })
    .fill("Общая физика");
  await page.getByRole("button", { name: "Сохранить предмет" }).click();
  await expect(
    page.locator(".live-subject-row").filter({ hasText: "Общая физика" }),
  ).toBeVisible();
  expect(
    data.calls.find((call) => call.path.endsWith("/decision"))?.body,
  ).toEqual({ decision: "rejected", reason: "Укажите источник" });
  expect(
    data.calls.some(
      (call) =>
        call.path.startsWith("/library-subjects/") && call.method === "PATCH",
    ),
  ).toBe(true);
});

test("registration, profile editing and password checks use the real account", async ({
  page,
}) => {
  const data = await liveFixture(page, { verified: false });
  await page.goto("/register");
  await page.getByLabel("Имя и фамилия").fill("Елена Иванова");
  await page.getByLabel("Электронная почта").fill("elena@example.test");
  await page.getByLabel("Пароль", { exact: true }).fill("server-password");
  await page
    .getByRole("button", { name: "Создать аккаунт", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "Подтверждение почты" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Меню пользователя" }).click();
  await page.getByRole("menuitem", { name: "Мой профиль" }).click();
  await page.getByLabel("Имя и фамилия").fill("Елена Студентова");
  await page.getByLabel("Факультет", { exact: true }).fill("");
  await page
    .getByRole("button", { name: "Сохранить профиль", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Елена Студентова" }),
  ).toBeVisible();
  expect(
    data.calls.find((call) => call.path === "/me" && call.method === "PATCH")
      ?.body,
  ).toEqual({ display_name: "Елена Студентова", faculty: null });
  await page.getByLabel("Текущий пароль", { exact: true }).fill("incorrect");
  await page
    .getByLabel("Новый пароль", { exact: true })
    .fill("new-server-password");
  await page
    .getByRole("button", { name: "Изменить пароль", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Неверный текущий пароль",
  );
  await expect(page.locator("body")).not.toContainText("fixture-request");
  await page
    .getByLabel("Текущий пароль", { exact: true })
    .fill("server-password");
  await page
    .getByRole("button", { name: "Изменить пароль", exact: true })
    .click();
  await expect(page.getByRole("status")).toContainText("Пароль изменён");
});

test("verification consumes one token without auth, cleans URL/referrer and updates the signed-in profile", async ({
  page,
}) => {
  const data = await liveFixture(page, { verified: false });
  await liveLogin(page);
  await page.goto("/verify-email?token=verify-fixture-token");
  await expect(
    page.getByRole("heading", { name: "Почта подтверждена", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/verify-email$/);
  await expect(page.locator('meta[name="referrer"]')).toHaveAttribute(
    "content",
    "no-referrer",
  );
  await expect(
    page.getByRole("region", { name: "Подтверждение почты" }),
  ).toHaveCount(0);
  const verification = data.calls.filter(
    (call) => call.path === "/auth/email-verification/confirm",
  );
  expect(verification).toHaveLength(1);
  expect(verification[0].body).toEqual({ token: "verify-fixture-token" });
  expect(verification[0].auth).toBeUndefined();
  expect(
    await page.evaluate(() =>
      JSON.stringify([
        Object.values(localStorage),
        Object.values(sessionStorage),
      ]),
    ),
  ).not.toContain("verify-fixture-token");
  await page.goto("/verify-email?token=invalid");
  await expect(page.getByRole("alert")).toContainText("Ссылка недействительна");
  await expect(page).toHaveURL(/\/verify-email$/);
});

test("mail unavailability hides the unavailable resend action", async ({
  page,
}) => {
  await liveFixture(page, { verified: false, mailUnavailable: true });
  await liveLogin(page);
  await page
    .getByRole("button", { name: "Отправить ещё раз", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("Отправка писем");
  await expect(
    page.getByRole("button", { name: "Отправить ещё раз", exact: true }),
  ).toHaveCount(0);
});

test("password reset removes its token, revokes local session and returns to real login", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  await page.goto("/reset-password?token=reset-fixture-token");
  await expect(page).toHaveURL(/\/reset-password$/);
  await page
    .getByLabel("Новый пароль", { exact: true })
    .fill("replacement-password");
  await page.getByLabel("Повторите пароль").fill("replacement-password");
  await page
    .getByRole("button", { name: "Сохранить новый пароль", exact: true })
    .click();
  await expect(page).toHaveURL(/\/login$/);
  expect(
    await page.evaluate(() => sessionStorage.getItem("studyvault.api.user.v1")),
  ).toBeNull();
  expect(
    data.calls.find((call) => call.path === "/auth/password-reset/confirm")
      ?.body,
  ).toEqual({
    token: "reset-fixture-token",
    new_password: "replacement-password",
  });
  await page
    .getByRole("button", { name: "Забыли пароль?", exact: true })
    .click();
  await page.getByLabel("Электронная почта").fill("unknown@example.test");
  await page
    .getByRole("button", { name: "Отправить ссылку", exact: true })
    .click();
  await expect(page.locator(".auth-form [role=status]")).toContainText(
    "Если адрес зарегистрирован",
  );
});

test("expired optional auth falls back to guest library and clears private state", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  data.expire();
  await page.getByLabel("Поиск материалов").fill("SQL");
  await expect(
    page.getByRole("button", { name: "Войти", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "SQL: понятные соединения", exact: true }),
  ).toBeVisible();
  await expect
    .poll(() => data.calls.some((call) => call.path === "/auth/refresh"))
    .toBe(true);
  expect(
    await page.evaluate(() => sessionStorage.getItem("studyvault.api.user.v1")),
  ).toBeNull();
  expect(
    data.calls.filter((call) => call.path === "/library").at(-1)?.auth,
  ).toBeUndefined();
  await page
    .getByRole("navigation", { name: "Основная навигация" })
    .getByRole("button", { name: "Мои материалы", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Войдите в аккаунт" }),
  ).toBeVisible();
});

test("cached moderator role cannot grant access; profile comes from me", async ({
  page,
}) => {
  const data = await liveFixture(page, { role: "student", signedIn: true });
  await page.addInitScript(
    (user) =>
      sessionStorage.setItem(
        "studyvault.api.user.v1",
        JSON.stringify({ ...user, role: "moderator" }),
      ),
    data.user,
  );
  await page.goto("/moderation");
  await expect(
    page.getByRole("heading", { name: "Раздел модератора" }),
  ).toBeVisible();
  await expect(
    page
      .getByRole("navigation", { name: "Основная навигация" })
      .getByRole("button", { name: "Модерация", exact: true }),
  ).toHaveCount(0);
  expect(data.calls.some((call) => call.path === "/moderation/queue")).toBe(
    false,
  );
  expect(
    data.calls.some((call) => call.path === "/me" && call.method === "GET"),
  ).toBe(true);
});

test("account deletion requires a correct password then clears data and session", async ({
  page,
}) => {
  const data = await liveFixture(page);
  await liveLogin(page);
  await page.goto("/profile");
  await page
    .getByRole("button", { name: "Удалить аккаунт", exact: true })
    .click();
  await page.getByLabel("Пароль для удаления").fill("incorrect");
  await page
    .getByRole("button", { name: "Удалить навсегда", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Неверный текущий пароль",
  );
  await page.getByLabel("Пароль для удаления").fill("server-password");
  await page
    .getByRole("button", { name: "Удалить навсегда", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Войти", exact: true }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => sessionStorage.getItem("studyvault.api.user.v1")),
  ).toBeNull();
  expect(
    data.calls
      .filter((call) => call.path === "/me" && call.method === "DELETE")
      .at(-1)?.body,
  ).toEqual({ password: "server-password" });
});

test("editor uploads directly without Bearer and published edits return to moderation", async ({
  page,
}) => {
  const data = await liveFixture(page);
  const uploads: string[] = [];
  await page.route("https://files.example.test/**", async (route) => {
    uploads.push(route.request().headers().authorization || "anonymous");
    await route.fulfill({ status: 200, body: "" });
  });
  await liveLogin(page);
  await page.goto(`/edit/${ids.own}`);
  await page.getByLabel("Прикрепить файл", { exact: true }).setInputFiles({
    name: "lecture.pdf",
    mimeType: "application/pdf",
    buffer: Buffer.from("%PDF-1.4"),
  });
  await expect(page.locator(".api-files .attachment")).toContainText(
    "lecture.pdf",
  );
  expect(uploads).toEqual(["anonymous"]);
  data.publishOwn();
  await page.reload();
  await expect(page.getByText("Опубликован", { exact: true })).toBeVisible();
  await page.getByLabel("Название материала").fill("Исправленный конспект");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page.getByText("На проверке", { exact: true })).toBeVisible();
});

test("a tag failure keeps the saved note and retry updates it without creating a duplicate", async ({
  page,
}) => {
  const data = await liveFixture(page, { failTagOnce: true });
  await liveLogin(page);
  await page.goto("/new");
  await page
    .getByLabel("Название материала")
    .fill("Сохранённый частично конспект");
  await page
    .getByLabel("Текст материала", { exact: true })
    .fill("Текст уже на сервере");
  await page.getByLabel("Теги", { exact: true }).fill("SQL");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`/edit/${ids.own}$`));
  await expect(page.getByRole("alert")).toContainText(
    "Материал сохранён, но не удалось применить теги",
  );
  await expect(page.getByLabel("Название материала")).toHaveValue(
    "Сохранённый частично конспект",
  );
  await page.getByLabel("Теги", { exact: true }).fill("SQL");
  await page.getByRole("button", { name: "Сохранить", exact: true }).click();
  await expect
    .poll(
      () =>
        data.calls.filter((call) => call.path === `/notes/${ids.own}/tags`)
          .length,
    )
    .toBe(1);
  expect(
    data.calls.filter(
      (call) => call.path === "/notes" && call.method === "POST",
    ),
  ).toHaveLength(1);
  expect(
    data.calls.filter(
      (call) => call.path === `/notes/${ids.own}` && call.method === "PATCH",
    ),
  ).toHaveLength(1);
});

test("moderator approves and takes down with a reason; rating own material is blocked", async ({
  page,
}) => {
  const data = await liveFixture(page, { role: "moderator" });
  await liveLogin(page);
  await page.goto("/moderation");
  await page.getByRole("button", { name: "Одобрить", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Все материалы проверены" }),
  ).toBeVisible();
  await page.goto(`/notes/${ids.material}`);
  await page
    .getByRole("button", { name: "Снять с публикации", exact: true })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Снять с публикации",
    exact: true,
  });
  await expect(
    dialog.getByRole("button", { name: "Снять с публикации", exact: true }),
  ).toBeDisabled();
  await page
    .getByLabel("Причина снятия", { exact: true })
    .fill("Некорректные формулы");
  await dialog
    .getByRole("button", { name: "Снять с публикации", exact: true })
    .click();
  await expect(page).toHaveURL(/\/moderation$/);
  expect(
    data.calls.find((call) => call.path.endsWith("/unpublish"))?.body,
  ).toEqual({ reason: "Некорректные формулы" });
  data.publishOwn();
  await page.goto(`/notes/${ids.own}`);
  await expect(
    page.getByRole("button", { name: "Оценить на 5", exact: true }),
  ).toBeDisabled();
});
