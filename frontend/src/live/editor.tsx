import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Code2,
  Italic,
  List,
  ListOrdered,
  Quote,
  Undo2,
  Redo2,
} from "lucide-react";
import { api } from "../api/client";
import { ApiFiles } from "../api/files";
import {
  ErrorBox,
  Dialog,
  Heading,
  Loading,
  LoginRequired,
  statuses,
  useLive,
} from "./common";
import type { Models } from "./common";

export function MaterialEditor({ id }: { id?: string }) {
  const { user, go, run, busy, notice } = useLive();
  const [title, setTitle] = useState(""),
    [summary, setSummary] = useState(""),
    [kind, setKind] = useState<Models["MaterialKind"]>("note"),
    [semester, setSemester] = useState(""),
    [subject, setSubject] = useState(""),
    [personal, setPersonal] = useState(""),
    [tags, setTags] = useState(""),
    [pinned, setPinned] = useState(false),
    [subjectTitle, setSubjectTitle] = useState("");
  const loaded = useRef(false);
  const baseline = useRef<string | null>(null);
  const dirty = useRef(false);
  const [, refreshEditor] = useState(0);
  const editor = useEditor({
    extensions: [StarterKit],
    content: { type: "doc", content: [{ type: "paragraph" }] },
    onUpdate: () => refreshEditor((value) => value + 1),
    onSelectionUpdate: () => refreshEditor((value) => value + 1),
    editorProps: {
      attributes: {
        class: "prose editor-prose",
        role: "textbox",
        "aria-label": "Текст материала",
      },
    },
  });
  const snapshot = JSON.stringify({
    title,
    summary,
    kind,
    semester,
    subject,
    personal,
    tags,
    pinned,
    content: editor?.getJSON(),
  });
  dirty.current = baseline.current !== null && baseline.current !== snapshot;
  const blocker = useBlocker({
    shouldBlockFn: ({ current, next }) =>
      current.pathname !== next.pathname && dirty.current,
    enableBeforeUnload: () => dirty.current,
    withResolver: true,
  });
  useEffect(() => {
    editor?.setEditable(!busy);
  }, [editor, busy]);
  useEffect(() => {
    if (!id && editor && baseline.current === null) {
      baseline.current = snapshot;
      refreshEditor((value) => value + 1);
    }
  }, [id, editor, snapshot]);
  const note = useQuery({
    queryKey: ["live", "edit", id, user?.id],
    queryFn: () => api.note(id!),
    enabled: !!id && !!user,
    retry: false,
  });
  const subjects = useQuery({
    queryKey: ["live", "subjects", user?.id],
    queryFn: api.librarySubjects,
    enabled: !!user,
    retry: false,
  });
  const personalSubjects = useQuery({
    queryKey: ["live", "personal-subjects", user?.id],
    queryFn: api.subjects,
    enabled: !!user,
    retry: false,
  });
  useEffect(() => {
    if (!note.data || !editor || loaded.current) return;
    const data = note.data;
    loaded.current = true;
    setTitle(data.title);
    setSummary(data.summary || "");
    setKind(data.kind);
    setSemester(data.semester?.toString() || "");
    setSubject(data.library_subject_id || "");
    setPersonal(data.subject_id || "");
    setPinned(data.is_pinned);
    setTags(data.tags.map((tag) => tag.name).join(", "));
    editor.commands.setContent(data.content);
    baseline.current = JSON.stringify({
      title: data.title,
      summary: data.summary || "",
      kind: data.kind,
      semester: data.semester?.toString() || "",
      subject: data.library_subject_id || "",
      personal: data.subject_id || "",
      tags: data.tags.map((tag) => tag.name).join(", "),
      pinned: data.is_pinned,
      content: editor.getJSON(),
    });
  }, [note.data, editor]);
  if (!user) return <LoginRequired />;
  if (id && note.isPending) return <Loading />;
  if (note.error) return <ErrorBox error={note.error} />;
  const status = note.data?.status || "draft";
  const save = async (submit: boolean) => {
    if (!editor || !title.trim()) return;
    if (submit && (!subject || !editor.getText().trim())) {
      notice("Выберите общий предмет и добавьте текст материала.");
      return;
    }
    if (submit && !user.email_verified) {
      notice("Подтвердите почту, чтобы отправить материал на проверку.");
      return;
    }
    const names = [
      ...new Set(
        tags
          .split(",")
          .map((value) => value.trim())
          .filter(Boolean),
      ),
    ];
    if (names.some((name) => name.length > 50)) {
      notice("Название тега не должно превышать 50 символов.");
      return;
    }
    let createdId: string | undefined;
    const result = await run(
      async () => {
        const body = {
          title: title.trim(),
          summary: summary.trim() || null,
          kind,
          semester: semester ? Number(semester) : null,
          library_subject_id: subject || null,
          subject_id: personal || null,
          is_pinned: pinned,
          content: editor.getJSON(),
        };
        let saved = id
          ? await api.updateNote(id, body)
          : await api.createNote(body);
        createdId = saved.id;
        try {
          const existing = await api.tags();
          const ids: string[] = [];
          for (const name of names) {
            const found = existing.items.find((tag) => tag.name === name);
            ids.push(found?.id || (await api.createTag(name)).id);
          }
          saved = await api.setTags(saved.id, ids);
        } catch (error) {
          throw new Error(
            `Материал сохранён, но не удалось применить теги. ${error instanceof Error ? error.message : "Повторите сохранение."}`,
          );
        }
        if (submit && saved.status !== "pending") await api.submit(saved.id);
        return saved;
      },
      submit
        ? "Материал отправлен на проверку"
        : status === "published"
          ? "Изменения сохранены. Материал снова проходит проверку."
          : "Материал сохранён",
    );
    if (result.ok) {
      baseline.current = snapshot;
      dirty.current = false;
      go(submit ? "/my" : `/edit/${result.data.id}`);
    } else if (!id && createdId) {
      baseline.current = snapshot;
      dirty.current = false;
      go(`/edit/${createdId}`);
    }
  };
  return (
    <>
      <Heading
        title={id ? "Редактировать материал" : "Новый материал"}
        description="Сохраните конспект для себя или поделитесь им с сообществом."
      />
      <fieldset
        className="editor-layout live-editor-fields"
        disabled={busy}
        aria-label="Редактор материала"
      >
        <section className="editor-card">
          <span className={`pill live-status ${status}`}>
            {statuses[status]}
          </span>
          {note.data?.rejection_reason && (
            <div className="info-box">
              Причина отклонения: {note.data.rejection_reason}
            </div>
          )}
          {status === "published" && (
            <div className="info-box">
              После изменения текста или вложений материал снова отправится на
              проверку.
            </div>
          )}
          <form
            onSubmit={(event) => {
              event.preventDefault();
              void save(false);
            }}
          >
            <label className="field-label">
              Название материала
              <input
                required
                maxLength={300}
                className="title-input"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
              />
            </label>
            <label className="field-label">
              Краткое описание
              <textarea
                maxLength={500}
                value={summary}
                onChange={(event) => setSummary(event.target.value)}
              />
            </label>
            <div className="editor-toolbar">
              <button
                type="button"
                aria-label="Полужирный"
                aria-pressed={editor?.isActive("bold") || false}
                title="Полужирный (Ctrl+B)"
                onClick={() => editor?.chain().focus().toggleBold().run()}
              >
                <Bold size={17} />
              </button>
              <button
                type="button"
                aria-label="Курсив"
                aria-pressed={editor?.isActive("italic") || false}
                title="Курсив (Ctrl+I)"
                onClick={() => editor?.chain().focus().toggleItalic().run()}
              >
                <Italic size={17} />
              </button>
              <button
                type="button"
                aria-label="Список"
                aria-pressed={editor?.isActive("bulletList") || false}
                onClick={() => editor?.chain().focus().toggleBulletList().run()}
              >
                <List size={17} />
              </button>
              <button
                type="button"
                aria-label="Блок кода"
                aria-pressed={editor?.isActive("codeBlock") || false}
                onClick={() => editor?.chain().focus().toggleCodeBlock().run()}
              >
                <Code2 size={17} />
              </button>
              <button
                type="button"
                aria-label="Заголовок раздела"
                aria-pressed={
                  editor?.isActive("heading", { level: 2 }) || false
                }
                onClick={() =>
                  editor?.chain().focus().toggleHeading({ level: 2 }).run()
                }
              >
                H2
              </button>
              <button
                type="button"
                aria-label="Нумерованный список"
                aria-pressed={editor?.isActive("orderedList") || false}
                onClick={() =>
                  editor?.chain().focus().toggleOrderedList().run()
                }
              >
                <ListOrdered size={17} />
              </button>
              <button
                type="button"
                aria-label="Цитата"
                aria-pressed={editor?.isActive("blockquote") || false}
                onClick={() => editor?.chain().focus().toggleBlockquote().run()}
              >
                <Quote size={17} />
              </button>
              <span className="live-toolbar-divider" />
              <button
                type="button"
                aria-label="Отменить изменение"
                disabled={!editor?.can().undo()}
                onClick={() => editor?.chain().focus().undo().run()}
              >
                <Undo2 size={17} />
              </button>
              <button
                type="button"
                aria-label="Повторить изменение"
                disabled={!editor?.can().redo()}
                onClick={() => editor?.chain().focus().redo().run()}
              >
                <Redo2 size={17} />
              </button>
            </div>
            <EditorContent editor={editor} />
            <div className="live-editor-status">
              <span>
                {editor?.getText().trim().split(/\s+/).filter(Boolean).length ||
                  0}{" "}
                слов
              </span>
              <span aria-live="polite">
                {busy
                  ? "Сохраняем…"
                  : dirty.current
                    ? "Есть несохранённые изменения"
                    : id
                      ? "Все изменения сохранены"
                      : "Новый черновик"}
              </span>
            </div>
            <label className="field-label">
              Теги
              <input
                maxLength={500}
                placeholder="SQL, экзамен, индексы"
                value={tags}
                onChange={(event) => setTags(event.target.value)}
              />
            </label>
            <div className="live-actions">
              <button
                className="button secondary"
                disabled={busy || !title.trim()}
              >
                Сохранить
              </button>
              {(status === "draft" || status === "rejected") && (
                <button
                  type="button"
                  className="button primary"
                  disabled={
                    busy || !user.email_verified || !title.trim() || !subject
                  }
                  title={!user.email_verified ? "Подтвердите почту" : ""}
                  onClick={() => void save(true)}
                >
                  Отправить на проверку
                </button>
              )}
              {status === "pending" && (
                <button
                  type="button"
                  className="button secondary"
                  disabled={busy}
                  onClick={() =>
                    void run(
                      () => api.withdraw(id!),
                      "Материал возвращён в черновики",
                    )
                  }
                >
                  Забрать в черновики
                </button>
              )}
            </div>
          </form>
          {id && (
            <button
              className="text-button live-delete"
              disabled={busy}
              onClick={async () => {
                if (!window.confirm("Удалить материал и его вложения?")) return;
                const result = await run(
                  () => api.deleteNote(id),
                  "Материал удалён",
                );
                if (result.ok) {
                  baseline.current = snapshot;
                  dirty.current = false;
                  go("/my");
                }
              }}
            >
              Удалить материал
            </button>
          )}
        </section>
        <aside className="editor-settings">
          <section className="aside-panel">
            <h3>О материале</h3>
            <ErrorBox error={subjects.error} />
            <label className="field-label">
              Общий предмет
              <select
                aria-label="Общий предмет"
                value={subject}
                onChange={(event) => setSubject(event.target.value)}
              >
                <option value="">Выберите предмет</option>
                {subjects.data?.items.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.title}
                  </option>
                ))}
              </select>
            </label>
            <label className="field-label">
              Тип материала
              <select
                value={kind}
                onChange={(event) => setKind(event.target.value as typeof kind)}
              >
                <option value="note">Конспект</option>
                <option value="cheat_sheet">Шпаргалка</option>
              </select>
            </label>
            <label className="field-label">
              Семестр
              <select
                value={semester}
                onChange={(event) => setSemester(event.target.value)}
              >
                <option value="">Не указан</option>
                {Array.from({ length: 12 }, (_, i) => (
                  <option key={i + 1} value={i + 1}>
                    {i + 1}
                  </option>
                ))}
              </select>
            </label>
            <label className="field-label">
              Личный предмет
              <select
                value={personal}
                onChange={(event) => setPersonal(event.target.value)}
              >
                <option value="">Без личного предмета</option>
                {personalSubjects.data?.items.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.title}
                  </option>
                ))}
              </select>
            </label>
            <form
              onSubmit={async (event) => {
                event.preventDefault();
                const result = await run(
                  () => api.createSubject({ title: subjectTitle.trim() }),
                  "Личный предмет создан",
                );
                if (result.ok) {
                  setPersonal(result.data.id);
                  setSubjectTitle("");
                }
              }}
            >
              <label className="field-label">
                Новый личный предмет
                <input
                  maxLength={200}
                  required
                  value={subjectTitle}
                  onChange={(event) => setSubjectTitle(event.target.value)}
                />
              </label>
              <button
                className="button secondary"
                disabled={busy || !subjectTitle.trim()}
              >
                Создать личный предмет
              </button>
            </form>
            <label className="live-check">
              <input
                type="checkbox"
                checked={pinned}
                onChange={(event) => setPinned(event.target.checked)}
              />
              Закрепить у себя
            </label>
          </section>
          {id ? (
            <ApiFiles userId={user.id} noteId={id} userFacing />
          ) : (
            <section className="aside-panel">
              <h3>Вложения</h3>
              <p className="muted">
                Сначала сохраните материал, затем прикрепите до 3 файлов по 2
                МБ.
              </p>
            </section>
          )}
        </aside>
      </fieldset>
      {blocker.status === "blocked" && (
        <Dialog title="Несохранённые изменения" close={() => blocker.reset()}>
          <h2>Сохранить изменения?</h2>
          <p>
            В материале есть изменения, которые ещё не сохранены. Если уйти
            сейчас, они будут потеряны.
          </p>
          <div className="live-actions">
            <button className="button primary" onClick={() => blocker.reset()}>
              Остаться в редакторе
            </button>
            <button
              className="button secondary"
              onClick={() => {
                dirty.current = false;
                blocker.proceed();
              }}
            >
              Уйти без сохранения
            </button>
          </div>
        </Dialog>
      )}
    </>
  );
}
