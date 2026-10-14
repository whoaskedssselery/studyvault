import { Fragment, useState } from "react";
import type { ReactNode } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Bookmark,
  Download,
  Edit,
  FileText,
  Star,
} from "lucide-react";
import { api } from "../api/client";
import { formatDate } from "../data";
import { downloadText } from "../downloads";
import {
  Avatar,
  Dialog,
  ErrorBox,
  Loading,
  NextPage,
  kindLabel,
  ratingLabel,
  safeFileUrl,
  statuses,
  useLive,
} from "./common";
import type { Models } from "./common";

type Node = {
  type?: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: Node[];
  marks?: { type: string; attrs?: Record<string, unknown> }[];
};
export function RichDocument({ content }: { content: Models["TiptapDoc"] }) {
  function render(node: Node, depth = 0): ReactNode {
    if (depth > 40) return null;
    if (node.type === "text")
      return (node.marks || []).reduce<ReactNode>((text, mark) => {
        if (mark.type === "bold") return <strong>{text}</strong>;
        if (mark.type === "italic") return <em>{text}</em>;
        if (mark.type === "code") return <code>{text}</code>;
        if (mark.type === "strike") return <s>{text}</s>;
        if (mark.type === "link" && typeof mark.attrs?.href === "string") {
          try {
            const link = new URL(mark.attrs.href);
            if (["http:", "https:"].includes(link.protocol))
              return (
                <a href={link.href} target="_blank" rel="noopener noreferrer">
                  {text}
                </a>
              );
          } catch {}
        }
        return text;
      }, node.text || "");
    const children = node.content?.map((child, index) => (
      <Fragment key={index}>{render(child, depth + 1)}</Fragment>
    ));
    if (node.type === "paragraph") return <p>{children}</p>;
    if (node.type === "heading")
      return node.attrs?.level === 3 ? (
        <h3>{children}</h3>
      ) : (
        <h2>{children}</h2>
      );
    if (node.type === "bulletList") return <ul>{children}</ul>;
    if (node.type === "orderedList") return <ol>{children}</ol>;
    if (node.type === "listItem") return <li>{children}</li>;
    if (node.type === "blockquote") return <blockquote>{children}</blockquote>;
    if (node.type === "codeBlock")
      return (
        <pre>
          <code>{children}</code>
        </pre>
      );
    if (node.type === "hardBreak") return <br />;
    if (node.type === "horizontalRule") return <hr />;
    return children;
  }
  return <div className="prose">{render(content as Node)}</div>;
}
function markdown(node: Node, depth = 0): string {
  if (depth > 40) return "";
  if (node.type === "text") return node.text || "";
  const text =
    node.content?.map((child) => markdown(child, depth + 1)).join("") || "";
  if (node.type === "heading")
    return `${"#".repeat(Number(node.attrs?.level) === 3 ? 3 : 2)} ${text}\n\n`;
  if (node.type === "codeBlock")
    return `\n\x60\x60\x60\n${text}\n\x60\x60\x60\n\n`;
  if (node.type === "paragraph") return `${text}\n\n`;
  if (node.type === "listItem") return `- ${text.trim()}\n`;
  if (node.type === "hardBreak") return "\n";
  return text;
}
export function MaterialPage({ id }: { id: string }) {
  const { user, go, run, busy, requireUser } = useLive();
  const [body, setBody] = useState(""),
    [editing, setEditing] = useState<string | null>(null),
    [editBody, setEditBody] = useState(""),
    [takedown, setTakedown] = useState(false),
    [reason, setReason] = useState("");
  const material = useQuery({
    queryKey: ["live", "material", id, user?.id],
    queryFn: () => api.material(id),
    retry: false,
    refetchOnWindowFocus: false,
  });
  const comments = useInfiniteQuery({
    queryKey: ["live", "comments", id, user?.id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api.comments(id, pageParam),
    getNextPageParam: (page) => page.next_cursor || undefined,
    enabled: material.data?.status === "published",
    retry: false,
  });
  if (material.isPending) return <Loading />;
  if (material.error)
    return (
      <ErrorBox error={material.error} retry={() => void material.refetch()} />
    );
  const item = material.data!;
  const verified = !!user?.email_verified;
  const own = user?.id === item.author.id;
  return (
    <>
      <div className="live-actions live-reader-actions">
        <button className="back-link" onClick={() => go("/")}>
          <ArrowLeft size={16} />
          Назад в библиотеку
        </button>
        <button
          className="button secondary"
          onClick={() =>
            downloadText(
              `studyvault-${id}.md`,
              `# ${item.title}\n\n${item.summary || ""}\n\n${markdown(item.content as Node)}`,
              "text/markdown;charset=utf-8",
            )
          }
        >
          <Download size={16} />
          Скачать Markdown
        </button>
        {own && (
          <button
            className="button secondary"
            onClick={() => go(`/edit/${id}`)}
          >
            <Edit size={16} />
            Редактировать
          </button>
        )}
        {user?.role === "moderator" && item.status === "published" && (
          <button
            className="button secondary"
            onClick={() => setTakedown(true)}
          >
            Снять с публикации
          </button>
        )}
      </div>
      <div className="reader-layout">
        <article className="reader">
          <div className="reader-meta">
            <span className="subject-label purple">
              {item.subject?.title || "Без предмета"}
            </span>
            <span>
              {kindLabel(item.kind)}
              {item.semester ? ` · ${item.semester} семестр` : ""}
            </span>
            {item.status !== "published" && (
              <span className="pill">{statuses[item.status]}</span>
            )}
          </div>
          <h1>{item.title}</h1>
          <p className="reader-description">{item.summary}</p>
          <button
            className="reader-author"
            onClick={() => go(`/authors/${item.author.id}`)}
          >
            <Avatar author={item.author} />
            <div>
              <strong>{item.author.display_name}</strong>
              <span>
                {formatDate(item.created_at)} · {item.views} просмотров
              </span>
            </div>
          </button>
          {item.rejection_reason && (
            <div className="info-box">
              <strong>Причина отклонения: </strong>
              {item.rejection_reason}
            </div>
          )}
          <RichDocument content={item.content} />
          <div className="tag-row reader-tags">
            {item.tags.map((tag) => (
              <span key={tag}>#{tag}</span>
            ))}
          </div>
          {item.attachments.length > 0 && (
            <section className="attachments">
              <h2>Вложения</h2>
              {item.attachments.map((file) => (
                <button
                  key={file.id}
                  className="attachment"
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const response =
                        item.status === "published"
                          ? await api.publicAttachment(id, file.id)
                          : await api.attachment(file.id);
                      const anchor = document.createElement("a");
                      anchor.href = safeFileUrl(response.download_url);
                      anchor.target = "_blank";
                      anchor.rel = "noopener noreferrer";
                      anchor.click();
                    })
                  }
                >
                  <FileText size={18} />
                  <span>
                    {file.file_name}
                    <small>{Math.ceil(file.size_bytes / 1024)} КБ</small>
                  </span>
                  <Download size={17} />
                </button>
              ))}
            </section>
          )}
          {item.status === "published" && (
            <section className="comments">
              <h2>
                Обсуждение <span>{item.comments_count}</span>
              </h2>
              <ErrorBox
                error={comments.error}
                retry={() => void comments.refetch()}
              />
              {user ? (
                <form
                  onSubmit={async (event) => {
                    event.preventDefault();
                    if (!verified) return;
                    const result = await run(
                      () => api.addComment(id, body.trim()),
                      "Комментарий добавлен",
                    );
                    if (result.ok) setBody("");
                  }}
                >
                  <textarea
                    aria-label="Комментарий"
                    placeholder={
                      verified
                        ? "Что думаете об этом материале?"
                        : "Подтвердите почту, чтобы комментировать"
                    }
                    required
                    maxLength={2000}
                    disabled={!verified}
                    value={body}
                    onChange={(event) => setBody(event.target.value)}
                  />
                  <button
                    className="button primary"
                    disabled={busy || !verified || !body.trim()}
                  >
                    Отправить комментарий
                  </button>
                </form>
              ) : (
                <p className="muted">
                  Чтобы участвовать в обсуждении,{" "}
                  <button className="text-button" onClick={() => go("/login")}>
                    войдите
                  </button>
                  .
                </p>
              )}
              {comments.data?.pages
                .flatMap((page) => page.items)
                .map((comment) => (
                  <article className="comment" key={comment.id}>
                    <Avatar author={comment.author} />
                    <div>
                      <button
                        className="text-button"
                        onClick={() => go(`/authors/${comment.author.id}`)}
                      >
                        {comment.author.display_name}
                      </button>
                      <time>
                        {formatDate(comment.created_at)}
                        {comment.edited_at ? " · изменён" : ""}
                      </time>
                      {editing === comment.id ? (
                        <form
                          onSubmit={async (event) => {
                            event.preventDefault();
                            const result = await run(
                              () =>
                                api.editComment(comment.id, editBody.trim()),
                              "Комментарий обновлён",
                            );
                            if (result.ok) setEditing(null);
                          }}
                        >
                          <textarea
                            aria-label="Изменить комментарий"
                            maxLength={2000}
                            required
                            value={editBody}
                            onChange={(event) =>
                              setEditBody(event.target.value)
                            }
                          />
                          <div className="live-actions">
                            <button
                              className="button primary"
                              disabled={busy || !verified || !editBody.trim()}
                            >
                              Сохранить комментарий
                            </button>
                            <button
                              type="button"
                              className="button secondary"
                              onClick={() => setEditing(null)}
                            >
                              Отмена
                            </button>
                          </div>
                        </form>
                      ) : (
                        <p>{comment.body}</p>
                      )}
                      <div className="live-actions">
                        {user?.id === comment.author.id && (
                          <button
                            className="text-button"
                            disabled={!verified || busy}
                            onClick={() => {
                              setEditing(comment.id);
                              setEditBody(comment.body);
                            }}
                          >
                            Изменить
                          </button>
                        )}
                        {(user?.id === comment.author.id ||
                          user?.role === "moderator") && (
                          <button
                            className="text-button"
                            disabled={busy}
                            onClick={() =>
                              window.confirm("Удалить комментарий?") &&
                              void run(
                                () => api.deleteComment(comment.id),
                                "Комментарий удалён",
                              )
                            }
                          >
                            Удалить комментарий
                          </button>
                        )}
                      </div>
                    </div>
                  </article>
                ))}
              <NextPage
                hasNext={comments.hasNextPage}
                loading={comments.isFetchingNextPage}
                load={() => void comments.fetchNextPage()}
              />
            </section>
          )}
        </article>
        <aside className="reader-aside">
          <section className="aside-panel">
            <div className="big-rating">
              <Star />
              <strong>{ratingLabel(item.rating)}</strong>
              <span>{item.rating.count} оценок сообщества</span>
            </div>
            {item.status === "published" && (
              <>
                <p>
                  {own
                    ? "Вы не можете оценить свой материал."
                    : !user
                      ? "Войдите, чтобы поставить оценку."
                      : !verified
                        ? "Подтвердите почту, чтобы оценивать материалы."
                        : "Было полезно? Оцените материал."}
                </p>
                <div className="star-picker">
                  {[1, 2, 3, 4, 5].map((value) => (
                    <button
                      key={value}
                      aria-label={`Оценить на ${value}`}
                      aria-pressed={item.my_rating === value}
                      disabled={busy || !verified || own}
                      onClick={() =>
                        void run(() => api.rate(id, value), "Оценка сохранена")
                      }
                    >
                      <Star
                        size={25}
                        fill={
                          (item.my_rating || 0) >= value
                            ? "currentColor"
                            : "none"
                        }
                      />
                    </button>
                  ))}
                </div>
                <button
                  className="button secondary full"
                  disabled={busy}
                  onClick={() =>
                    requireUser() &&
                    void run(() => api.bookmark(id, item.bookmarked))
                  }
                >
                  <Bookmark size={17} />
                  {item.bookmarked
                    ? "Убрать из избранного"
                    : "Сохранить материал"}
                </button>
              </>
            )}
          </section>
          <section className="aside-panel author-panel">
            <Avatar author={item.author} large />
            <h3>{item.author.display_name}</h3>
            <p>{item.author.faculty || "Студент StudyVault"}</p>
            <button
              className="text-button"
              onClick={() => go(`/authors/${item.author.id}`)}
            >
              Профиль автора →
            </button>
          </section>
        </aside>
      </div>
      {takedown && (
        <Dialog
          title="Снять с публикации"
          close={() => {
            if (!busy) setTakedown(false);
          }}
        >
          <h2>Снять материал с публикации</h2>
          <p>Укажите причину. Она будет доступна автору.</p>
          <textarea
            aria-label="Причина снятия"
            maxLength={1000}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
          <div className="live-actions">
            <button
              className="button primary"
              disabled={busy || !reason.trim()}
              onClick={async () => {
                const result = await run(
                  () => api.unpublish(id, reason.trim()),
                  "Материал снят с публикации",
                );
                if (result.ok) {
                  setTakedown(false);
                  go("/moderation");
                }
              }}
            >
              Снять с публикации
            </button>
            <button
              className="button secondary"
              onClick={() => setTakedown(false)}
            >
              Отмена
            </button>
          </div>
        </Dialog>
      )}
    </>
  );
}
