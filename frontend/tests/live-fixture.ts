import type { Page } from "@playwright/test";
import type { components } from "../src/api/schema";
type M = components["schemas"];
export const ids = {
  user: "11111111-1111-4111-8111-111111111111",
  author: "22222222-2222-4222-8222-222222222222",
  material: "33333333-3333-4333-8333-333333333333",
  own: "44444444-4444-4444-8444-444444444444",
  subject: "55555555-5555-4555-8555-555555555555",
  queue: "66666666-6666-4666-8666-666666666666",
  file: "77777777-7777-4777-8777-777777777777",
  comment: "88888888-8888-4888-8888-888888888888",
};
const date = "2026-10-14T10:00:00Z";
export async function liveFixture(
  page: Page,
  options: {
    role?: M["User"]["role"];
    verified?: boolean;
    mailUnavailable?: boolean;
    resend429?: boolean;
    signedIn?: boolean;
  } = {},
) {
  let user: M["User"] = {
    id: ids.user,
    email: "student@example.test",
    email_verified: options.verified ?? true,
    display_name: "Анна Студентова",
    faculty: "Информационные технологии",
    role: options.role || "student",
    created_at: date,
  };
  const author: M["Author"] = {
    id: ids.author,
    display_name: "Максим Волков",
    faculty: "Информатика",
  };
  const subject: M["LibrarySubject"] = {
    id: ids.subject,
    title: "Базы данных",
    color: "#7655ce",
    materials_count: 1,
  };
  let own: M["Note"] = {
    id: ids.own,
    subject_id: null,
    title: "Мой черновик",
    is_pinned: false,
    kind: "note",
    status: "draft",
    library_subject_id: ids.subject,
    tags: [],
    summary: "Личный материал",
    semester: 2,
    published_at: null,
    views: 0,
    rejection_reason: null,
    content: {
      type: "doc",
      content: [
        {
          type: "paragraph",
          content: [{ type: "text", text: "Текст личного конспекта." }],
        },
      ],
    },
    created_at: date,
    updated_at: date,
  };
  let material: M["Material"] = {
    id: ids.material,
    title: "SQL: понятные соединения",
    summary: "JOIN без сложностей",
    kind: "cheat_sheet",
    semester: 2,
    subject: { id: ids.subject, title: subject.title, color: subject.color },
    author,
    rating: { average: null, count: 0 },
    views: 5,
    comments_count: 0,
    bookmarks_count: 0,
    published_at: date,
    my_rating: null,
    bookmarked: false,
    status: "published",
    rejection_reason: null,
    content: {
      type: "doc",
      content: [
        {
          type: "heading",
          attrs: { level: 2 },
          content: [{ type: "text", text: "Соединяем таблицы" }],
        },
        {
          type: "paragraph",
          content: [{ type: "text", text: "SQL возвращает связанные строки." }],
        },
      ],
    },
    tags: ["SQL"],
    attachments: [],
    created_at: date,
    updated_at: date,
  };
  const tags: M["Tag"][] = [],
    comments: M["Comment"][] = [],
    files: M["Attachment"][] = [],
    subjects = [subject];
  let queue: M["QueueItem"][] = [
    {
      id: ids.queue,
      title: "Материал на проверке",
      summary: "Проверьте источник",
      kind: "note",
      semester: 2,
      subject: material.subject,
      author,
      submitted_at: date,
      created_at: date,
    },
  ];
  let logged = options.signedIn || false,
    expired = false,
    resend = 0,
    deleted = false;
  const calls: {
    path: string;
    method: string;
    auth?: string;
    body: any;
    query: string;
  }[] = [];
  const item = (): M["LibraryItem"] => ({
    id: material.id,
    title: material.title,
    summary: material.summary,
    kind: material.kind,
    semester: material.semester,
    subject: material.subject,
    author: material.author,
    rating: material.rating,
    views: material.views,
    comments_count: material.comments_count,
    bookmarks_count: material.bookmarks_count,
    published_at: material.published_at!,
    my_rating: material.my_rating,
    bookmarked: material.bookmarked,
    tags: material.tags,
    snippet: null,
  });
  await page.route(/\/(backend-api|v1)\//, async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname.replace(/^\/(backend-api|v1)/, ""),
      method = request.method(),
      auth = request.headers().authorization;
    let body: any = null;
    try {
      body = request.postDataJSON();
    } catch {}
    calls.push({ path, method, auth, body, query: url.search });
    const json = (value: unknown, status = 200) =>
      route.fulfill({ status, json: value });
    const empty = () => route.fulfill({ status: 204 });
    const problem = (
      status: number,
      code: string,
      detail = code,
      retry?: string,
    ) =>
      route.fulfill({
        status,
        contentType: "application/problem+json",
        headers: {
          "X-Request-ID": "fixture-request",
          ...(retry ? { "Retry-After": retry } : {}),
        },
        body: JSON.stringify({
          type: `https://example.test/errors#${code}`,
          title: "Request failed",
          status,
          code,
          detail,
          request_id: "fixture-request",
        }),
      });
    if (path === "/auth/login" || path === "/auth/register") {
      if (body.password === "wrong-password")
        return problem(401, "invalid_credentials");
      logged = true;
      expired = false;
      if (path.endsWith("register"))
        user = { ...user, email: body.email, display_name: body.display_name };
      const response: M["AuthResponse"] = {
        access_token: "live-access-token",
        expires_in: 900,
        user,
      };
      return json(response, path.endsWith("register") ? 201 : 200);
    }
    if (path === "/auth/refresh")
      return expired
        ? problem(401, "invalid_token")
        : json({
            access_token: "live-access-token",
            expires_in: 900,
          } satisfies M["TokenResponse"]);
    if (path === "/auth/logout") {
      logged = false;
      return empty();
    }
    if (path === "/auth/password-reset")
      return options.mailUnavailable
        ? problem(503, "email_unavailable")
        : empty();
    if (path === "/auth/password-reset/confirm") {
      if (body.token !== "reset-fixture-token")
        return problem(401, "invalid_token");
      logged = false;
      return empty();
    }
    if (path === "/auth/email-verification/confirm") {
      if (body.token !== "verify-fixture-token")
        return problem(401, "invalid_token");
      user.email_verified = true;
      return empty();
    }
    const publicRead =
      method === "GET" &&
      (/^\/library(?:\/|$)/.test(path) ||
        path === "/library-subjects" ||
        path === "/ranking/authors" ||
        path.startsWith("/authors/"));
    if (expired && auth) return problem(401, "unauthorized");
    if (!publicRead && (!logged || !auth || deleted))
      return problem(401, "unauthorized");
    if (path === "/me") {
      if (method === "PATCH") {
        user = { ...user, ...body };
        return json(user);
      }
      if (method === "DELETE") {
        if (body.password !== "server-password")
          return problem(403, "invalid_credentials");
        deleted = true;
        logged = false;
        return empty();
      }
      return json(user);
    }
    if (path === "/me/password")
      return body.current_password === "server-password"
        ? empty()
        : problem(403, "invalid_credentials");
    if (path === "/me/email-verification") {
      resend++;
      if (options.mailUnavailable) return problem(503, "email_unavailable");
      if (options.resend429 && resend === 1)
        return problem(429, "rate_limited", "Try later", "2");
      return empty();
    }
    if (path === "/library-subjects") {
      if (method === "POST") {
        const created = {
          id: "99999999-9999-4999-8999-999999999999",
          ...body,
          materials_count: 0,
        };
        subjects.push(created);
        return json(created, 201);
      }
      return json({ items: subjects });
    }
    if (path.startsWith("/library-subjects/")) {
      const index = subjects.findIndex((subject) => path.endsWith(subject.id));
      if (index < 0) return problem(404, "not_found");
      if (method === "DELETE") {
        subjects.splice(index, 1);
        return empty();
      }
      subjects[index] = { ...subjects[index], ...body };
      return json(subjects[index]);
    }
    if (path === "/library" || path === "/bookmarks") {
      const shown = item();
      if (url.searchParams.has("q"))
        shown.snippet =
          "<mark>SQL</mark> &lt;script&gt;window.bad=1&lt;/script&gt;";
      if (!auth) {
        shown.bookmarked = false;
        shown.my_rating = null;
      }
      const items =
        (path === "/bookmarks" && !material.bookmarked) ||
        url.searchParams.get("q") === "нет-результатов"
          ? []
          : [shown];
      return json({
        items: url.searchParams.has("cursor") ? [] : items,
        next_cursor: url.searchParams.has("cursor") ? null : "library-next",
      } satisfies M["LibraryPage"]);
    }
    if (path === `/library/${ids.material}`) return json(material);
    if (path === `/library/${ids.own}`)
      return json({
        ...material,
        ...own,
        subject: material.subject,
        author: {
          id: user.id,
          display_name: user.display_name,
          faculty: user.faculty,
        },
        attachments: files,
      });
    if (path === `/library/${ids.queue}`)
      return json({
        ...material,
        id: ids.queue,
        title: "Материал на проверке",
        status: "pending",
      });
    if (path === `/library/${ids.material}/bookmark`) {
      material.bookmarked = method === "PUT";
      return empty();
    }
    if (path === `/library/${ids.material}/rating`) {
      if (!user.email_verified) return problem(403, "email_not_verified");
      material.my_rating = body.value;
      material.rating = { average: body.value, count: 1 };
      return json({
        rating: material.rating,
        my_rating: body.value,
      } satisfies M["RatingResult"]);
    }
    if (path === `/library/${ids.material}/comments`) {
      if (method === "POST") {
        if (!user.email_verified) return problem(403, "email_not_verified");
        const comment: M["Comment"] = {
          id: ids.comment,
          body: body.body,
          author: {
            id: user.id,
            display_name: user.display_name,
            faculty: user.faculty,
          },
          created_at: date,
          edited_at: null,
        };
        comments.push(comment);
        material.comments_count = comments.length;
        return json(comment, 201);
      }
      return json({
        items: comments,
        next_cursor: null,
      } satisfies M["CommentPage"]);
    }
    if (path === `/comments/${ids.comment}`) {
      if (method === "DELETE") {
        comments.length = 0;
        material.comments_count = 0;
        return empty();
      }
      comments[0] = { ...comments[0], body: body.body, edited_at: date };
      return json(comments[0]);
    }
    if (/^\/library\/[^/]+\/attachments\//.test(path))
      return json({
        attachment: {
          id: ids.file,
          note_id: ids.material,
          file_name: "lecture.pdf",
          mime_type: "application/pdf",
          size_bytes: 20,
          created_at: date,
        },
        download_url: "https://files.example.test/download.pdf",
      } satisfies M["AttachmentDownload"]);
    if (path === "/notes") {
      if (method === "POST") {
        own = { ...own, ...body };
        return json(own, 201);
      }
      return json({ items: [own], next_cursor: null } satisfies M["NotePage"]);
    }
    if (path === `/notes/${ids.own}`) {
      if (method === "PATCH") {
        own = {
          ...own,
          ...body,
          status: own.status === "published" ? "pending" : own.status,
        };
        return json(own);
      }
      if (method === "DELETE") return empty();
      return json(own);
    }
    if (path === `/notes/${ids.own}/tags`) {
      own.tags = tags.filter((tag) => body.tag_ids.includes(tag.id));
      return json(own);
    }
    if (path === `/notes/${ids.own}/submit`) {
      if (!user.email_verified) return problem(403, "email_not_verified");
      own.status = "pending";
      return json(own);
    }
    if (path === `/notes/${ids.own}/withdraw`) {
      own.status = "draft";
      return json(own);
    }
    if (path === "/subjects")
      return method === "POST"
        ? json(
            {
              id: ids.subject,
              title: body.title,
              color: null,
              semester: null,
              created_at: date,
            } satisfies M["Subject"],
            201,
          )
        : json({ items: [] });
    if (path === "/tags") {
      if (method === "POST") {
        const tag: M["Tag"] = { id: ids.subject, name: body.name };
        tags.push(tag);
        return json(tag, 201);
      }
      return json({ items: tags });
    }
    if (path === "/attachments") {
      if (method === "GET") return json({ items: files });
      const file: M["Attachment"] = { id: ids.file, ...body, created_at: date };
      files.push(file);
      return json(
        {
          attachment: file,
          upload_url: "https://files.example.test/upload.pdf",
        } satisfies M["AttachmentUpload"],
        201,
      );
    }
    if (path === `/attachments/${ids.file}`) {
      if (method === "DELETE") {
        files.length = 0;
        return empty();
      }
      return json({
        attachment: files[0],
        download_url: "https://files.example.test/download.pdf",
      } satisfies M["AttachmentDownload"]);
    }
    if (path === "/moderation/queue")
      return user.role === "moderator"
        ? json({ items: queue, next_cursor: null } satisfies M["QueuePage"])
        : problem(403, "forbidden");
    if (path === `/moderation/${ids.queue}/decision`) {
      queue = [];
      return json({
        id: ids.queue,
        status: body.decision === "approved" ? "published" : "rejected",
      } satisfies M["DecisionResult"]);
    }
    if (path === `/moderation/${ids.material}/unpublish`) {
      material.status = "rejected";
      material.rejection_reason = body.reason;
      return json({
        id: ids.material,
        status: "rejected",
      } satisfies M["DecisionResult"]);
    }
    const stats: M["AuthorStats"] = {
      author,
      materials_count: 1,
      ratings_count: material.rating.count,
      rating: material.rating,
      views: material.views,
    };
    if (path === "/ranking/authors")
      return json({
        items: [{ ...stats, rank: 1 } satisfies M["RankedAuthor"]],
      });
    if (path === `/authors/${ids.author}`) return json(stats);
    return problem(404, "not_found", "Fixture route is not implemented");
  });
  return {
    calls,
    get user() {
      return user;
    },
    get own() {
      return own;
    },
    expire() {
      expired = true;
    },
    publishOwn() {
      own.status = "published";
    },
  };
}
export async function liveLogin(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Электронная почта").fill("student@example.test");
  await page.getByLabel("Пароль", { exact: true }).fill("server-password");
  await page.getByRole("button", { name: "Войти", exact: true }).click();
  await page
    .getByRole("button", { name: "Меню пользователя", exact: true })
    .waitFor();
}
