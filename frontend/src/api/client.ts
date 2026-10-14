import createClient from "openapi-fetch";
import type { components, paths } from "./schema";
import { apiBaseUrl } from "../runtime";
export type ApiUser = components["schemas"]["User"];
export type ApiNote = components["schemas"]["Note"];
export type ApiSubject = components["schemas"]["Subject"];
export type ApiTag = components["schemas"]["Tag"];
export type ApiAttachment = components["schemas"]["Attachment"];
const userKey = "studyvault.api.user.v1";
let token = "",
  generation = 0;
let refreshing: Promise<void> | null = null;
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public retryAfter?: string | null,
  ) {
    super(message);
  }
}
export function sessionUser(): ApiUser | null {
  try {
    const user = JSON.parse(sessionStorage.getItem(userKey) || "null");
    return user &&
      typeof user.id === "string" &&
      typeof user.display_name === "string" &&
      typeof user.email === "string"
      ? user
      : null;
  } catch {
    return null;
  }
}
function forget() {
  token = "";
  generation++;
  try {
    sessionStorage.removeItem(userKey);
  } catch {}
  window.dispatchEvent(new Event("studyvault-api-session-ended"));
}
async function send(request: Request): Promise<Response> {
  try {
    return await fetch(request, {
      signal: AbortSignal.any([request.signal, AbortSignal.timeout(8000)]),
    });
  } catch {
    throw new ApiError(
      0,
      "network",
      "API не отвечает. Запустите Docker Compose или проверьте адрес сервера.",
    );
  }
}
async function refresh(): Promise<void> {
  if (refreshing) return refreshing;
  const before = generation;
  refreshing = (async () => {
    const response = await send(
      new Request(new URL(`${apiBaseUrl}/auth/refresh`, location.origin), {
        method: "POST",
        credentials: "include",
      }),
    );
    if (!response.ok) {
      if (response.status === 401) forget();
      throw new ApiError(
        response.status,
        "session_expired",
        response.status === 429
          ? "Слишком много попыток обновления сессии. Повторите позже."
          : "Сессия истекла. Войдите снова.",
        response.headers.get("retry-after"),
      );
    }
    const data: components["schemas"]["TokenResponse"] = await response.json();
    if (before !== generation)
      throw new ApiError(
        401,
        "session_changed",
        "Сессия изменилась. Повторите вход.",
      );
    if (typeof data.access_token !== "string")
      throw new ApiError(
        502,
        "invalid_response",
        "API вернул ответ без access-токена.",
      );
    token = data.access_token;
  })().finally(() => {
    refreshing = null;
  });
  return refreshing;
}
const client = createClient<paths>({
  baseUrl: apiBaseUrl,
  credentials: "include",
  fetch: async (request) => {
    const before = generation,
      original = request.clone();
    if (token) request.headers.set("Authorization", `Bearer ${token}`);
    let response = await send(request);
    if (
      response.status === 401 &&
      !new URL(request.url).pathname.includes("/auth/") &&
      sessionUser() &&
      before === generation
    ) {
      try {
        await refresh();
        original.headers.set("Authorization", `Bearer ${token}`);
      } catch (error) {
        const path = new URL(request.url).pathname.slice(apiBaseUrl.length);
        const publicRead =
          request.method === "GET" &&
          /^\/(library(?:\/.*)?|library-subjects|ranking\/authors|authors\/[^/]+)$/.test(
            path,
          );
        if (!publicRead || !(error instanceof ApiError) || error.status !== 401)
          throw error;
        original.headers.delete("Authorization");
      }
      response = await send(original);
      if (response.status === 401) forget();
    }
    return response;
  },
});
const publicClient = createClient<paths>({
  baseUrl: apiBaseUrl,
  credentials: "omit",
  fetch: send,
});
async function result<T>(
  operation: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let value: { data?: T; error?: unknown; response: Response };
  try {
    value = await operation;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw new ApiError(
      502,
      "invalid_response",
      "Сервер вернул некорректный ответ.",
    );
  }
  if (!value.response.ok) {
    const error = value.error as
      | {
          code?: string;
          detail?: string;
          error?: { code?: string; message?: string };
        }
      | undefined;
    const code = error?.code || error?.error?.code || "http_error";
    const messages: Record<string, string> = {
      invalid_credentials:
        value.response.status === 403
          ? "Неверный текущий пароль."
          : "Неверная почта или пароль.",
      email_taken: "Эта почта уже зарегистрирована.",
      rate_limited: "Слишком много попыток. Повторите позже.",
      limit_reached: error?.detail
        ? `Достигнут лимит: ${error.detail}`
        : "Достигнут лимит заметок, предметов, тегов или файлов. Удалите ненужные объекты и повторите попытку.",
      email_not_verified:
        "Подтвердите почту, чтобы публиковать материалы, комментировать и ставить оценки.",
      email_unavailable: "Отправка писем сейчас недоступна. Попробуйте позже.",
      forbidden: "Недостаточно прав для этого действия.",
      invalid_transition:
        "Статус материала уже изменился. Обновите список и повторите действие.",
      invalid_token: "Ссылка недействительна, просрочена или уже использована.",
      not_found: "Объект не найден или принадлежит другому пользователю.",
      search_unavailable:
        "Поиск временно недоступен. Заметки доступны в списке.",
      storage_unavailable: "Хранилище файлов временно недоступно.",
      not_uploaded: "Файл ещё не загружен. Повторите загрузку.",
      api_unavailable: "API не запущен. Демоверсия продолжает работать.",
    };
    throw new ApiError(
      value.response.status,
      code,
      messages[code] ||
        error?.detail ||
        error?.error?.message ||
        `Ошибка API: HTTP ${value.response.status}`,
      value.response.headers.get("retry-after"),
    );
  }
  if (value.data === undefined && value.response.status !== 204)
    throw new ApiError(
      502,
      "invalid_response",
      "API вернул пустой ответ вместо JSON.",
    );
  return value.data as T;
}
function remember(auth: components["schemas"]["AuthResponse"]) {
  generation++;
  token = auth.access_token;
  try {
    sessionStorage.setItem(userKey, JSON.stringify(auth.user));
  } catch {}
  return auth.user;
}
function rememberUser(user: ApiUser) {
  try {
    sessionStorage.setItem(userKey, JSON.stringify(user));
  } catch {}
  return user;
}
export const api = {
  clearSession: forget,
  me: async () => rememberUser(await result(client.GET("/me"))),
  login: async (email: string, password: string) =>
    remember(
      await result(client.POST("/auth/login", { body: { email, password } })),
    ),
  register: async (email: string, password: string, display_name: string) =>
    remember(
      await result(
        client.POST("/auth/register", {
          body: { email, password, display_name },
        }),
      ),
    ),
  restore: async () => {
    const user = sessionUser();
    const before = generation;
    if (user && !token) await refresh();
    if (!user || before !== generation) return null;
    const current = await result(client.GET("/me"));
    return before === generation ? rememberUser(current) : null;
  },
  logout: async () => {
    if (refreshing) await refreshing.catch(() => {});
    forget();
    await result(client.POST("/auth/logout"));
  },
  subjects: () => result(client.GET("/subjects")),
  createSubject: (body: components["schemas"]["SubjectInput"]) =>
    result(client.POST("/subjects", { body })),
  tags: () => result(client.GET("/tags")),
  createTag: (name: string) => result(client.POST("/tags", { body: { name } })),
  notes: (
    query: NonNullable<paths["/notes"]["get"]["parameters"]["query"]> = {},
  ) => result(client.GET("/notes", { params: { query } })),
  note: (id: string) =>
    result(client.GET("/notes/{id}", { params: { path: { id } } })),
  createNote: (body: components["schemas"]["NoteInput"]) =>
    result(client.POST("/notes", { body })),
  updateNote: (id: string, body: components["schemas"]["NotePatch"]) =>
    result(client.PATCH("/notes/{id}", { params: { path: { id } }, body })),
  deleteNote: (id: string) =>
    result(client.DELETE("/notes/{id}", { params: { path: { id } } })),
  setTags: (id: string, tag_ids: string[]) =>
    result(
      client.PUT("/notes/{id}/tags", {
        params: { path: { id } },
        body: { tag_ids },
      }),
    ),
  search: (q: string, subject_id?: string) =>
    result(
      client.GET("/search", {
        params: { query: { q, subject_id, limit: 20 } },
      }),
    ),
  graph: () => result(client.GET("/graph")),
  backlinks: (id: string) =>
    result(client.GET("/notes/{id}/backlinks", { params: { path: { id } } })),
  attachments: (note_id: string) =>
    result(client.GET("/attachments", { params: { query: { note_id } } })),
  registerAttachment: (body: components["schemas"]["AttachmentInput"]) =>
    result(client.POST("/attachments", { body })),
  attachment: (id: string) =>
    result(client.GET("/attachments/{id}", { params: { path: { id } } })),
  deleteAttachment: (id: string) =>
    result(client.DELETE("/attachments/{id}", { params: { path: { id } } })),
  library: (
    query: NonNullable<paths["/library"]["get"]["parameters"]["query"]> = {},
  ) => result(client.GET("/library", { params: { query } })),
  librarySubjects: () => result(client.GET("/library-subjects")),
  createLibrarySubject: (body: components["schemas"]["LibrarySubjectInput"]) =>
    result(client.POST("/library-subjects", { body })),
  updateLibrarySubject: (
    id: string,
    body: components["schemas"]["LibrarySubjectPatch"],
  ) =>
    result(
      client.PATCH("/library-subjects/{id}", {
        params: { path: { id } },
        body,
      }),
    ),
  deleteLibrarySubject: (id: string) =>
    result(
      client.DELETE("/library-subjects/{id}", { params: { path: { id } } }),
    ),
  material: (id: string) =>
    result(client.GET("/library/{id}", { params: { path: { id } } })),
  publicAttachment: (id: string, attachment_id: string) =>
    result(
      client.GET("/library/{id}/attachments/{attachment_id}", {
        params: { path: { id, attachment_id } },
      }),
    ),
  comments: (id: string, cursor?: string) =>
    result(
      client.GET("/library/{id}/comments", {
        params: { path: { id }, query: { cursor, limit: 30 } },
      }),
    ),
  addComment: (id: string, body: string) =>
    result(
      client.POST("/library/{id}/comments", {
        params: { path: { id } },
        body: { body },
      }),
    ),
  editComment: (id: string, body: string) =>
    result(
      client.PATCH("/comments/{id}", {
        params: { path: { id } },
        body: { body },
      }),
    ),
  deleteComment: (id: string) =>
    result(client.DELETE("/comments/{id}", { params: { path: { id } } })),
  rate: (id: string, value: number) =>
    result(
      client.PUT("/library/{id}/rating", {
        params: { path: { id } },
        body: { value },
      }),
    ),
  bookmark: (id: string, saved: boolean) =>
    saved
      ? result(
          client.DELETE("/library/{id}/bookmark", { params: { path: { id } } }),
        )
      : result(
          client.PUT("/library/{id}/bookmark", { params: { path: { id } } }),
        ),
  bookmarks: (cursor?: string) =>
    result(
      client.GET("/bookmarks", { params: { query: { cursor, limit: 30 } } }),
    ),
  submit: (id: string) =>
    result(client.POST("/notes/{id}/submit", { params: { path: { id } } })),
  withdraw: (id: string) =>
    result(client.POST("/notes/{id}/withdraw", { params: { path: { id } } })),
  queue: (cursor?: string) =>
    result(
      client.GET("/moderation/queue", {
        params: { query: { cursor, limit: 30 } },
      }),
    ),
  decision: (id: string, decision: "approved" | "rejected", reason?: string) =>
    result(
      client.POST("/moderation/{id}/decision", {
        params: { path: { id } },
        body: { decision, reason },
      }),
    ),
  unpublish: (id: string, reason: string) =>
    result(
      client.POST("/moderation/{id}/unpublish", {
        params: { path: { id } },
        body: { reason },
      }),
    ),
  ranking: () =>
    result(
      client.GET("/ranking/authors", { params: { query: { limit: 100 } } }),
    ),
  author: (id: string) =>
    result(client.GET("/authors/{id}", { params: { path: { id } } })),
  profile: async (body: components["schemas"]["ProfilePatch"]) =>
    rememberUser(await result(client.PATCH("/me", { body }))),
  changePassword: (current_password: string, new_password: string) =>
    result(
      client.POST("/me/password", { body: { current_password, new_password } }),
    ),
  deleteAccount: async (password: string) => {
    await result(client.DELETE("/me", { body: { password } }));
    forget();
  },
  resendVerification: () => result(client.POST("/me/email-verification")),
  verifyEmail: (token: string) =>
    result(
      publicClient.POST("/auth/email-verification/confirm", {
        body: { token },
      }),
    ),
  resetPassword: (email: string) =>
    result(publicClient.POST("/auth/password-reset", { body: { email } })),
  confirmPasswordReset: async (token: string, new_password: string) => {
    await result(
      publicClient.POST("/auth/password-reset/confirm", {
        body: { token, new_password },
      }),
    );
    forget();
  },
};
