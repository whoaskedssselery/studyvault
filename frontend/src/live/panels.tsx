import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowUpRight,
  BookOpen,
  FileText,
  Search,
  ShieldCheck,
  X,
} from "lucide-react";
import { api } from "../api/client";
import { formatDate } from "../data";
import {
  Dialog,
  ErrorBox,
  Loading,
  statuses,
  useDebounced,
  useLive,
} from "./common";
import type { Models } from "./common";

function PanelHeading({
  title,
  description,
  close,
}: {
  title: string;
  description?: string;
  close: () => void;
}) {
  return (
    <div className="live-panel-heading">
      <div>
        <h2>{title}</h2>
        {description && <p>{description}</p>}
      </div>
      <button className="icon-button" aria-label="Закрыть окно" onClick={close}>
        <X size={20} />
      </button>
    </div>
  );
}

export function SearchPanel({ close }: { close: () => void }) {
  const { user, go } = useLive();
  const [value, setValue] = useState("");
  const [active, setActive] = useState(0);
  const q = useDebounced(value.trim());
  const results = useQuery({
    queryKey: ["live", "quick-search", user?.id, q],
    queryFn: () => api.library({ q: q || undefined, limit: 8 }),
    retry: false,
  });
  const items = results.data?.items || [];
  useEffect(() => setActive(0), [q]);
  const open = (id: string) => {
    close();
    go(`/notes/${id}`);
  };
  return (
    <Dialog title="Поиск по библиотеке" close={close}>
      <PanelHeading
        title="Найти материал"
        description="По названию, теме или тексту конспекта"
        close={close}
      />
      <label className="live-command-input">
        <Search size={20} />
        <input
          data-autofocus
          aria-label="Быстрый поиск материалов"
          role="combobox"
          aria-expanded="true"
          aria-autocomplete="list"
          aria-controls="quick-search-results"
          aria-activedescendant={
            items[active] ? `quick-result-${items[active].id}` : undefined
          }
          placeholder="Что будем изучать?"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          onKeyDown={(event) => {
            if (
              (event.key === "ArrowDown" || event.key === "ArrowUp") &&
              items.length
            ) {
              event.preventDefault();
              setActive(
                (index) =>
                  (index +
                    (event.key === "ArrowDown" ? 1 : -1) +
                    items.length) %
                  items.length,
              );
            }
            if (event.key === "Enter" && items[active] && value.trim() === q) {
              event.preventDefault();
              open(items[active].id);
            }
          }}
        />
      </label>
      <ErrorBox error={results.error} retry={() => void results.refetch()} />
      {results.isPending ? (
        <Loading />
      ) : (
        <div
          id="quick-search-results"
          role="listbox"
          aria-label="Найденные материалы"
          className="live-command-results"
        >
          {items.map((item, index) => (
            <button
              role="option"
              aria-selected={index === active}
              id={`quick-result-${item.id}`}
              key={item.id}
              onMouseMove={() => setActive(index)}
              onClick={() => open(item.id)}
            >
              <span className="live-panel-icon">
                <FileText size={19} />
              </span>
              <span>
                <strong>{item.title}</strong>
                <small>
                  {item.subject?.title || "Без предмета"} ·{" "}
                  {item.author.display_name}
                </small>
              </span>
              <ArrowUpRight size={17} />
            </button>
          ))}
        </div>
      )}
      {!results.isPending && !results.error && !items.length && (
        <div className="live-panel-empty">
          <Search size={30} />
          <h3>Ничего не нашлось</h3>
          <p>Попробуйте другой запрос или название предмета.</p>
        </div>
      )}
      <p className="live-keyboard-hint">
        <kbd>↑</kbd>
        <kbd>↓</kbd> выбор <kbd>Enter</kbd> открыть <kbd>Esc</kbd> закрыть
      </p>
    </Dialog>
  );
}

export function ActivityPanel({ close }: { close: () => void }) {
  const { user, go } = useLive();
  const notes = useQuery({
    queryKey: ["live", "activity", user?.id],
    queryFn: () => api.notes({ limit: 20 }),
    enabled: !!user,
    retry: false,
  });
  const descriptions = {
    draft: "Продолжите работу над конспектом",
    pending: "Ожидает решения модератора",
    published: "Материал доступен в библиотеке",
    rejected: "Посмотрите замечания и внесите исправления",
  };
  const visit = (path: string) => {
    close();
    go(path);
  };
  return (
    <Dialog title="Активность материалов" close={close} placement="drawer">
      <PanelHeading
        title="Активность материалов"
        description="Статусы ваших последних конспектов"
        close={close}
      />
      {!user ? (
        <div className="live-panel-empty">
          <ShieldCheck size={32} />
          <h3>Будьте в курсе публикаций</h3>
          <p>
            Войдите, чтобы видеть решения модераторов и продолжать работу над
            материалами.
          </p>
          <button className="button primary" onClick={() => visit("/login")}>
            Войти
          </button>
        </div>
      ) : (
        <>
          <ErrorBox error={notes.error} retry={() => void notes.refetch()} />
          {notes.isPending ? (
            <Loading />
          ) : (
            <div className="live-activity-list">
              {notes.data?.items.map((note) => (
                <button key={note.id} onClick={() => visit(`/edit/${note.id}`)}>
                  <span className={`pill live-status ${note.status}`}>
                    {statuses[note.status]}
                  </span>
                  <strong>{note.title}</strong>
                  <p>{descriptions[note.status]}</p>
                  <small>{formatDate(note.updated_at)}</small>
                </button>
              ))}
            </div>
          )}
          {notes.data?.items.length === 0 && (
            <div className="live-panel-empty">
              <BookOpen size={32} />
              <h3>Всё начинается с конспекта</h3>
              <p>
                Создайте материал — здесь появятся его статус и решения
                модераторов.
              </p>
              <button className="button primary" onClick={() => visit("/new")}>
                Создать материал
              </button>
            </div>
          )}
          {!!notes.data?.items.length && (
            <button
              className="button secondary full"
              onClick={() => visit("/my")}
            >
              Все мои материалы <ArrowUpRight size={17} />
            </button>
          )}
        </>
      )}
    </Dialog>
  );
}

export function SubjectsPanel({
  subjects,
  error,
  pending,
  retry,
  select,
  close,
}: {
  subjects: Models["LibrarySubject"][];
  error: unknown;
  pending: boolean;
  retry: () => void;
  select: (id: string) => void;
  close: () => void;
}) {
  return (
    <Dialog title="Предметы библиотеки" close={close}>
      <PanelHeading
        title="Выберите предмет"
        description="Соберите всё нужное к следующему занятию"
        close={close}
      />
      <ErrorBox error={error} retry={retry} />
      {pending ? (
        <Loading />
      ) : (
        <div className="live-subject-picker">
          <button onClick={() => select("")}>
            <BookOpen size={21} />
            <strong>Все предметы</strong>
            <small>Полная библиотека</small>
          </button>
          {subjects.map((subject) => (
            <button key={subject.id} onClick={() => select(subject.id)}>
              <span
                className="subject-dot"
                style={{
                  color: /^#[0-9a-f]{6}$/i.test(subject.color || "")
                    ? subject.color!
                    : "var(--purple)",
                }}
              />
              <strong>{subject.title}</strong>
              <small>{subject.materials_count} материалов</small>
            </button>
          ))}
        </div>
      )}
    </Dialog>
  );
}

export function HelpPanel({ close }: { close: () => void }) {
  return (
    <Dialog title="Как пользоваться StudyVault" close={close}>
      <PanelHeading
        title="Знания лучше вместе"
        description="StudyVault — библиотека, которую создают студенты"
        close={close}
      />
      <div className="live-help-content">
        <section>
          <BookOpen size={22} />
          <div>
            <h3>Находите нужное</h3>
            <p>
              Читайте конспекты без входа. Используйте поиск, предметы и
              фильтры, а полезное сохраняйте в избранное после входа.
            </p>
          </div>
        </section>
        <section>
          <FileText size={22} />
          <div>
            <h3>Делитесь знаниями</h3>
            <p>
              Создайте конспект или шпаргалку, выберите общий предмет и
              отправьте на проверку. Личный черновик виден только вам.
            </p>
          </div>
        </section>
        <section>
          <ShieldCheck size={22} />
          <div>
            <h3>Помогайте сообществу</h3>
            <p>
              Подтвердите почту, чтобы публиковать материалы, оставлять
              комментарии и оценивать чужие работы. Загружайте свои материалы и
              указывайте источники.
            </p>
          </div>
        </section>
      </div>
      <p className="live-help-note">
        Вложения: PDF, PNG, JPEG и WebP. До трёх файлов по 2 МБ на материал.
      </p>
      <p className="live-keyboard-hint">
        <kbd>Ctrl</kbd> + <kbd>K</kbd> быстрый поиск · <kbd>Esc</kbd> закрыть
        окно
      </p>
    </Dialog>
  );
}
