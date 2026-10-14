import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import {
  Avatar,
  Dialog,
  ErrorBox,
  Heading,
  Loading,
  LoginRequired,
  NextPage,
  kindLabel,
  useLive,
} from "./common";

export function ModerationPage() {
  const { user, go, run, busy } = useLive();
  const [rejectId, setRejectId] = useState<string | null>(null),
    [reason, setReason] = useState(""),
    [newTitle, setNewTitle] = useState(""),
    [newColor, setNewColor] = useState("#7655ce"),
    [editId, setEditId] = useState<string | null>(null),
    [editTitle, setEditTitle] = useState(""),
    [editColor, setEditColor] = useState("");
  const moderator = user?.role === "moderator";
  const queue = useInfiniteQuery({
    queryKey: ["live", "queue", user?.id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api.queue(pageParam),
    getNextPageParam: (page) => page.next_cursor || undefined,
    enabled: moderator,
    retry: false,
  });
  const subjects = useQuery({
    queryKey: ["live", "subjects", user?.id],
    queryFn: api.librarySubjects,
    enabled: moderator,
    retry: false,
  });
  if (!user) return <LoginRequired />;
  if (!moderator)
    return (
      <div className="empty">
        <h1>Раздел модератора</h1>
        <p>Очередь доступна только модераторам сообщества.</p>
        <button className="button primary" onClick={() => go("/")}>
          В библиотеку
        </button>
      </div>
    );
  return (
    <>
      <Heading
        title="Модерация"
        description="Помогайте сохранять качество материалов библиотеки."
      />
      <ErrorBox error={queue.error} retry={() => void queue.refetch()} />
      {queue.isPending ? (
        <Loading />
      ) : (
        queue.data?.pages
          .flatMap((page) => page.items)
          .map((item) => (
            <article className="moderation-card" key={item.id}>
              <Avatar author={item.author} />
              <div>
                <button
                  className="moderation-title"
                  onClick={() => go(`/notes/${item.id}`)}
                >
                  {item.title}
                </button>
                <p>{item.summary}</p>
                <span className="small-text muted">
                  {item.author.display_name} ·{" "}
                  {item.subject?.title || "Без предмета"} ·{" "}
                  {kindLabel(item.kind)}
                </span>
              </div>
              <div className="moderation-actions">
                <button
                  className="button secondary"
                  onClick={() => go(`/notes/${item.id}`)}
                >
                  Прочитать
                </button>
                <button
                  className="button primary"
                  disabled={busy || item.author.id === user.id}
                  onClick={() =>
                    void run(
                      () => api.decision(item.id, "approved"),
                      "Материал опубликован",
                    )
                  }
                >
                  Одобрить
                </button>
                <button
                  className="button secondary"
                  disabled={busy || item.author.id === user.id}
                  onClick={() => {
                    setRejectId(item.id);
                    setReason("");
                  }}
                >
                  Отклонить
                </button>
                {item.author.id === user.id && (
                  <span className="small-text muted">
                    Свой материал рассматривает другой модератор.
                  </span>
                )}
              </div>
            </article>
          ))
      )}
      {queue.data?.pages[0].items.length === 0 && (
        <div className="empty">
          <h2>Все материалы проверены</h2>
          <p>Новые заявки появятся здесь.</p>
        </div>
      )}
      <NextPage
        hasNext={queue.hasNextPage}
        loading={queue.isFetchingNextPage}
        load={() => void queue.fetchNextPage()}
      />
      <section className="editor-card live-subject-admin">
        <h2>Общие предметы</h2>
        <ErrorBox error={subjects.error} />
        <form
          className="live-actions"
          onSubmit={async (event) => {
            event.preventDefault();
            const result = await run(
              () =>
                api.createLibrarySubject({
                  title: newTitle.trim(),
                  color: newColor || null,
                }),
              "Предмет создан",
            );
            if (result.ok) setNewTitle("");
          }}
        >
          <label className="field-label">
            Новый общий предмет
            <input
              required
              maxLength={200}
              value={newTitle}
              onChange={(event) => setNewTitle(event.target.value)}
            />
          </label>
          <label className="field-label">
            Цвет предмета
            <input
              type="color"
              value={newColor}
              onChange={(event) => setNewColor(event.target.value)}
            />
          </label>
          <button
            className="button primary"
            disabled={busy || !newTitle.trim()}
          >
            Создать предмет
          </button>
        </form>
        {subjects.data?.items.map((subject) => (
          <div key={subject.id} className="live-subject-row">
            {editId === subject.id ? (
              <form
                className="live-actions"
                onSubmit={async (event) => {
                  event.preventDefault();
                  const result = await run(
                    () =>
                      api.updateLibrarySubject(subject.id, {
                        title: editTitle.trim(),
                        color: editColor || null,
                      }),
                    "Предмет обновлён",
                  );
                  if (result.ok) setEditId(null);
                }}
              >
                <input
                  aria-label="Название общего предмета"
                  required
                  maxLength={200}
                  value={editTitle}
                  onChange={(event) => setEditTitle(event.target.value)}
                />
                <input
                  aria-label="Цвет общего предмета"
                  placeholder="#7655ce или пусто"
                  pattern="#[0-9a-fA-F]{6}"
                  value={editColor}
                  onChange={(event) => setEditColor(event.target.value)}
                />
                <button
                  className="button primary"
                  disabled={busy || !editTitle.trim()}
                >
                  Сохранить предмет
                </button>
                <button
                  type="button"
                  className="button secondary"
                  onClick={() => setEditId(null)}
                >
                  Отмена
                </button>
              </form>
            ) : (
              <>
                <strong>{subject.title}</strong>
                <span className="muted">
                  {subject.materials_count} материалов
                </span>
                <button
                  className="text-button"
                  onClick={() => {
                    setEditId(subject.id);
                    setEditTitle(subject.title);
                    setEditColor(subject.color || "");
                  }}
                >
                  Изменить предмет
                </button>
                <button
                  className="text-button"
                  disabled={busy}
                  onClick={() =>
                    window.confirm(
                      `Удалить общий предмет «${subject.title}»?`,
                    ) &&
                    void run(
                      () => api.deleteLibrarySubject(subject.id),
                      "Предмет удалён",
                    )
                  }
                >
                  Удалить предмет
                </button>
              </>
            )}
          </div>
        ))}
      </section>
      {rejectId && (
        <Dialog
          title="Отклонить материал"
          close={() => {
            if (!busy) setRejectId(null);
          }}
        >
          <h2>Причина отклонения</h2>
          <p>Подскажите автору, что нужно исправить.</p>
          <textarea
            aria-label="Причина отклонения"
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
                  () => api.decision(rejectId, "rejected", reason.trim()),
                  "Материал отклонён",
                );
                if (result.ok) setRejectId(null);
              }}
            >
              Отклонить материал
            </button>
            <button
              className="button secondary"
              onClick={() => setRejectId(null)}
            >
              Отмена
            </button>
          </div>
        </Dialog>
      )}
    </>
  );
}
