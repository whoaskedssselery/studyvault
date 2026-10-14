import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { ApiUser, ApiNote, ApiAttachment } from "../src/api/client";
const userId = "11111111-1111-4111-8111-111111111111";
const noteId = "22222222-2222-4222-8222-222222222222";
const subjectId = "33333333-3333-4333-8333-333333333333";
const tagId = "44444444-4444-4444-8444-444444444444";
const attachmentId = "55555555-5555-4555-8555-555555555555";
const nextId = "66666666-6666-4666-8666-666666666666";
const user: ApiUser = {
  id: userId,
  email: "api@example.test",
  display_name: "Студент API",
  email_verified: true,
  faculty: "Информационные технологии",
  role: "student",
  created_at: "2026-10-14T10:00:00Z",
};
async function setup(page: Page, expire = false) {
  const calls: {
    path: string;
    method: string;
    auth?: string;
    body: any;
    query: string;
  }[] = [];
  let current: ApiNote = {
    id: noteId,
    title: "Серверный конспект",
    subject_id: subjectId,
    is_pinned: false,
    kind: "note",
    status: "draft",
    library_subject_id: null,
    summary: null,
    semester: null,
    published_at: null,
    views: 0,
    rejection_reason: null,
    tags: [] as { id: string; name: string }[],
    created_at: "2026-10-14T10:00:00Z",
    updated_at: "2026-10-14T10:00:00Z",
    content: {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [{ type: "text", text: "Данные получены через HTTP API." }],
        },
      ],
    },
  };
  const files: ApiAttachment[] = [];
  await page.route("**/backend-api/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname.replace("/backend-api", ""),
      method = request.method(),
      auth = request.headers()["authorization"];
    let body: any = null;
    try {
      body = request.postDataJSON();
    } catch {}
    calls.push({ path, method, auth, body, query: url.search });
    const respond = (
      data: unknown,
      status = 200,
      headers?: Record<string, string>,
    ) => route.fulfill({ status, json: data, headers });
    if (path === "/auth/login" || path === "/auth/register")
      return respond(
        { user, access_token: "initial-access-secret", expires_in: 900 },
        path.endsWith("register") ? 201 : 200,
        {
          "set-cookie":
            "refresh_token=fixture-refresh; HttpOnly; SameSite=Lax; Path=/backend-api/auth",
        },
      );
    if (path === "/auth/refresh") {
      await new Promise((r) => setTimeout(r, 80));
      return respond({
        access_token: "rotated-access-secret",
        expires_in: 900,
      });
    }
    if (path === "/auth/logout") return route.fulfill({ status: 204 });
    if (expire && auth === "Bearer initial-access-secret")
      return respond(
        { error: { code: "unauthorized", message: "expired" } },
        401,
      );
    if (!auth)
      return respond(
        { error: { code: "unauthorized", message: "missing bearer token" } },
        401,
      );
    if (path === "/me") return respond(user);
    if (path === "/subjects")
      return respond(
        method === "GET"
          ? {
              items: [
                {
                  id: subjectId,
                  title: "Базы данных API",
                  color: "#7655ce",
                  semester: 2,
                  created_at: user.created_at,
                },
              ],
            }
          : { id: subjectId, ...body, created_at: user.created_at },
        method === "POST" ? 201 : 200,
      );
    if (path === "/tags")
      return respond(
        method === "GET"
          ? { items: [{ id: tagId, name: "SQL" }] }
          : { id: tagId, name: body.name },
        method === "POST" ? 201 : 200,
      );
    if (path === "/notes") {
      if (method === "POST") {
        current = { ...current, ...body };
        return respond(current, 201);
      }
      const { content, ...summary } = current;
      return respond(
        url.searchParams.has("cursor")
          ? {
              items: [{ ...summary, id: nextId, title: "Вторая страница API" }],
              next_cursor: null,
            }
          : { items: [summary], next_cursor: "opaque-next-page" },
      );
    }
    if (path === `/notes/${noteId}/tags`) {
      current = {
        ...current,
        tags: body.tag_ids.map((id: string) => ({ id, name: "SQL" })),
      };
      return respond(current);
    }
    if (path.endsWith("/backlinks")) return respond({ items: [] });
    if (path === `/notes/${noteId}` || path === `/notes/${nextId}`) {
      if (method === "PATCH") current = { ...current, ...body };
      if (method === "DELETE") return route.fulfill({ status: 204 });
      return respond(current);
    }
    if (path === "/search")
      return respond({
        items: [
          {
            id: noteId,
            title: current.title,
            subject_id: subjectId,
            updated_at: current.updated_at,
            snippet:
              "<mark>SQL</mark> &lt;script&gt;window.bad=1&lt;/script&gt;",
          },
        ],
      });
    if (path === "/graph")
      return respond({
        nodes: [
          { id: noteId, title: current.title },
          { id: nextId, title: "Связанная заметка" },
        ],
        edges: [{ source: noteId, target: nextId }],
      });
    if (path === "/attachments") {
      if (method === "GET") return respond({ items: files });
      const file = { id: files.length ? `55555555-5555-4555-8555-${String(files.length + 1).padStart(12, "0")}` : attachmentId, ...body, created_at: user.created_at };
      files.push(file);
      return respond(
        {
          attachment: file,
          upload_url: "https://files.example.test/notes.pdf",
        },
        201,
      );
    }
    if (path.startsWith("/attachments/")) {
      const index = files.findIndex(file => path === `/attachments/${file.id}`);
      if (index < 0) return respond({ type: "about:blank", title: "Not Found", status: 404, code: "not_found", detail: "File not found" }, 404);
      if (method === "DELETE") {
        files.splice(index, 1);
        return route.fulfill({ status: 204 });
      }
      return respond({
        attachment: files[index],
        download_url: "https://files.example.test/notes.pdf",
      });
    }
    return respond({ error: { code: "not_found", message: path } }, 404);
  });
  return calls;
}
async function login(page: Page) {
  await page.goto("/workspace");
  await page.getByLabel("Почта API").fill(user.email);
  await page.getByLabel("Пароль API").fill("server-password");
  await page.getByRole("button", { name: "Войти в API", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Мои знания." }),
  ).toBeVisible();
}

test("real API client logs in, paginates, saves Tiptap and registers subjects", async ({
  page,
}) => {
  const calls = await setup(page);
  await login(page);
  await page
    .getByRole("button", { name: "Загрузить ещё", exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: /Вторая страница API/ }),
  ).toBeVisible();
  expect(
    calls.some(
      (c) => c.path === "/notes" && c.query.includes("cursor=opaque-next-page"),
    ),
  ).toBe(true);
  await page
    .getByRole("button", { name: /Серверный конспект/ })
    .first()
    .click();
  await expect(page.getByLabel("Название заметки API")).toHaveValue(
    "Серверный конспект",
  );
  await page.getByLabel("Название заметки API").fill("Изменённая заметка");
  await page
    .getByRole("textbox", { name: "Текст заметки API" })
    .fill("Обновлённый текст");
  await page
    .getByRole("button", { name: "Сохранить в API", exact: true })
    .click();
  await expect(
    page.getByText("Заметка сохранена на сервере", { exact: true }),
  ).toBeVisible();
  const patch = calls.find((c) => c.method === "PATCH");
  expect(patch?.body.content.type).toBe("doc");
  expect(patch?.auth).toBe("Bearer initial-access-secret");
  expect(patch?.body.title).toBe("Изменённая заметка");
  await page.getByLabel("Новый предмет", { exact: true }).fill("Алгоритмы");
  await page
    .getByRole("button", { name: "Создать предмет", exact: true })
    .click();
  await expect(page.getByText("Предмет создан", { exact: true })).toBeVisible();
  expect(
    calls.some(
      (c) =>
        c.method === "POST" &&
        c.path === "/subjects" &&
        c.body.title === "Алгоритмы",
    ),
  ).toBe(true);
  await page.screenshot({ path: '../docs/assignment-2/screenshots/08-api-workspace.png', fullPage: true });
});
test("simultaneous 401s share one refresh and access tokens never enter storage", async ({
  page,
}) => {
  const calls = await setup(page, true);
  await login(page);
  await expect(
    page.getByRole("button", { name: /Серверный конспект/ }).first(),
  ).toBeVisible();
  expect(calls.filter((c) => c.path === "/auth/refresh")).toHaveLength(1);
  expect(calls.some((c) => c.auth === "Bearer rotated-access-secret")).toBe(
    true,
  );
  const storage = await page.evaluate(() =>
    JSON.stringify({
      local: { ...localStorage },
      session: { ...sessionStorage },
      cookie: document.cookie,
    }),
  );
  expect(storage).not.toContain("initial-access-secret");
  expect(storage).not.toContain("rotated-access-secret");
  expect(storage).not.toContain("fixture-refresh");
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Мои знания." }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Выйти из API", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Личный кабинет API." }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => sessionStorage.getItem("studyvault.api.user.v1")),
  ).toBeNull();
});
test("API search highlights safely, graph opens notes and tags use IDs", async ({
  page,
}) => {
  const calls = await setup(page);
  await login(page);
  await page.getByLabel("Поиск в API").fill("SQL");
  await page.getByRole("button", { name: "Найти в API", exact: true }).click();
  await expect(page.locator(".api-search-result mark")).toHaveText("SQL");
  expect(await page.evaluate(() => (window as any).bad)).toBeUndefined();
  await page.locator(".api-search-result").click();
  await expect(
    page.getByRole("checkbox", { name: "SQL", exact: true }),
  ).toBeVisible();
  await page.getByRole("checkbox", { name: "SQL", exact: true }).check();
  await page
    .getByRole("button", { name: "Сохранить теги", exact: true })
    .click();
  await expect
    .poll(() =>
      calls.some(
        (c) =>
          c.path.endsWith("/tags") &&
          c.method === "PUT" &&
          c.body.tag_ids[0] === tagId,
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Граф связей", exact: true }).click();
  await expect(
    page.getByRole("img", { name: "Граф заметок API" }),
  ).toBeVisible();
  await page
    .getByRole("button", {
      name: "Открыть заметку Связанная заметка",
      exact: true,
    })
    .click();
  await expect
    .poll(() => calls.some((c) => c.path === `/notes/${nextId}`))
    .toBe(true);
});
test("S3 PUT uses the original MIME and sends no Bearer token", async ({
  page,
}) => {
  const calls = await setup(page),
    puts: Record<string, string>[] = [];
  await page.route("https://files.example.test/**", async (route) => {
    puts.push(route.request().headers());
    await route.fulfill({ status: 200, body: "" });
  });
  await login(page);
  await page
    .getByRole("button", { name: /Серверный конспект/ })
    .first()
    .click();
  await page
    .getByLabel("Файл API", { exact: true })
    .setInputFiles({
      name: "notes.pdf",
      mimeType: "application/pdf",
      buffer: Buffer.from("%PDF-1.4\n%%EOF"),
    });
  await expect(page.locator(".api-files .attachment")).toContainText(
    "notes.pdf",
  );
  await expect.poll(() => puts.length).toBe(1);
  expect(puts[0]["content-type"]).toBe("application/pdf");
  expect(puts[0]["authorization"]).toBeUndefined();
  expect(
    calls.find((c) => c.path === "/attachments" && c.method === "POST")?.body,
  ).toMatchObject({
    note_id: noteId,
    mime_type: "application/pdf",
    file_name: "notes.pdf",
    size_bytes: 14,
  });
});
test("API attachments reject unsupported files and over 2 MB, cap at three and free a slot on deletion", async ({ page }) => {
  const calls = await setup(page);
  let uploads = 0;
  await page.route("https://files.example.test/**", async route => { uploads++; await route.fulfill({ status: 200, body: "" }); });
  await login(page);
  await page.getByRole("button", { name: /Серверный конспект/ }).first().click();
  const input = page.getByLabel("Файл API", { exact: true });
  await expect(input).toBeEnabled();
  await input.setInputFiles({ name: "animation.gif", mimeType: "image/gif", buffer: Buffer.from("GIF89a") });
  await expect(page.locator(".api-files [role=alert]")).toContainText("PDF, PNG, JPEG или WebP");
  await input.setInputFiles({ name: "too-large.pdf", mimeType: "application/pdf", buffer: Buffer.alloc(2 * 1024 * 1024 + 1) });
  await expect(page.locator(".api-files [role=alert]")).toContainText("до 2 МБ");
  expect(calls.filter(call => call.path === "/attachments" && call.method === "POST")).toHaveLength(0);
  expect(uploads).toBe(0);
  for (let count = 1; count <= 3; count++) {
    await expect(input).toBeEnabled();
    await input.setInputFiles({ name: `file-${count}.pdf`, mimeType: "application/pdf", buffer: count === 1 ? Buffer.alloc(2 * 1024 * 1024) : Buffer.from("%PDF-1.4") });
    await expect(page.locator(".api-files .attachment")).toHaveCount(count);
  }
  await expect(input).toBeDisabled();
  expect(uploads).toBe(3);
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Удалить файл file-1.pdf", exact: true }).click();
  await expect(page.locator(".api-files .attachment")).toHaveCount(2);
  await expect(input).toBeEnabled();
});

test("wrong credentials never refresh; auth rate limits show Retry-After", async ({
  page,
}) => {
  let status = 401,
    refreshes = 0;
  await page.route("**/backend-api/**", (route) => {
    if (route.request().url().endsWith("/auth/refresh")) refreshes++;
    return route.fulfill({
      status,
      contentType: "application/problem+json",
      headers: { "Retry-After": "30" },
      json: {
        type: "https://example.test/errors",
        title: status === 401 ? "Unauthorized" : "Too Many Requests",
        status,
        code: status === 401 ? "invalid_credentials" : "rate_limited",
        detail: "error",
      },
    });
  });
  await page.goto("/workspace");
  await page.getByLabel("Почта API").fill(user.email);
  await page.getByLabel("Пароль API").fill("wrong-password");
  await page.getByRole("button", { name: "Войти в API", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Неверная почта или пароль",
  );
  expect(refreshes).toBe(0);
  status = 429;
  await page.getByRole("button", { name: "Войти в API", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("30 сек.");
});

test('RFC 7807 quota errors keep the workspace and unsaved subject available', async ({ page }) => {
  await setup(page); await login(page);
  await page.route('**/backend-api/subjects', async route => {
    if (route.request().method() !== 'POST') return route.fallback();
    return route.fulfill({ status: 409, contentType: 'application/problem+json', body: JSON.stringify({ type: 'https://example.test/errors#limit_reached', title: 'Conflict', status: 409, code: 'limit_reached', detail: 'at most 200 subjects', request_id: 'quota-fixture' }) });
  });
  await page.getByLabel('Новый предмет', { exact: true }).fill('Не сохранённый предмет');
  await page.getByRole('button', { name: 'Создать предмет', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Достигнут лимит');
  await expect(page.getByLabel('Новый предмет', { exact: true })).toHaveValue('Не сохранённый предмет');
  await expect(page.getByRole('heading', { name: 'Мои знания.' })).toBeVisible();
  await expect(page.getByText('Предмет создан', { exact: true })).toHaveCount(0);
});

test('API registration sends display_name and deletion uses the note UUID', async ({ page }) => {
  const calls = await setup(page);
  await page.goto('/workspace');
  await page.getByRole('button', { name: 'Создать аккаунт', exact: true }).click();
  await page.getByLabel('Имя на сервере').fill('Тестовый студент');
  await page.getByLabel('Почта API').fill(user.email);
  await page.getByLabel('Пароль API').fill('server-password');
  await page.getByRole('button', { name: 'Зарегистрироваться в API', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Мои знания.' })).toBeVisible();
  expect(calls.find(c => c.path === '/auth/register')?.body.display_name).toBe('Тестовый студент');
  await page.getByRole('button', { name: /Серверный конспект/ }).first().click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Удалить из API', exact: true }).click();
  await expect(page.getByText('Заметка удалена с сервера', { exact: true })).toBeVisible();
  expect(calls.some(c => c.path === `/notes/${noteId}` && c.method === 'DELETE')).toBe(true);
});

test('an invalid refresh closes the private workspace and clears cached account data', async ({ page }) => {
  await setup(page); await login(page);
  await expect(page.getByRole('button', { name: /Серверный конспект/ }).first()).toBeVisible();
  await page.route('**/backend-api/auth/refresh', route => route.fulfill({ status: 401, json: { error: { code: 'invalid_token', message: 'expired refresh' } } }));
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Личный кабинет API.' })).toBeVisible();
  expect(await page.evaluate(() => sessionStorage.getItem('studyvault.api.user.v1'))).toBeNull();
  await expect(page.getByRole('button', { name: /Серверный конспект/ })).toHaveCount(0);
});
