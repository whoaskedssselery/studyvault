import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  Bookmark,
  FileText,
  MessageCircle,
  Plus,
  Search,
  Star,
} from "lucide-react";
import { api } from "../api/client";
import { formatDate } from "../data";
import {
  Avatar,
  ErrorBox,
  Heading,
  Loading,
  LoginRequired,
  NextPage,
  SafeSnippet,
  kindLabel,
  ratingLabel,
  statuses,
  useDebounced,
  useLive,
} from "./common";
import type { Models } from "./common";

export function MaterialCard({ item }: { item: Models["LibraryItem"] }) {
  const { go, run, busy, requireUser } = useLive();
  return (
    <article
      className="material-card live-material-card"
      onClick={(event) => {
        if (!(event.target as Element).closest("button, a"))
          go(`/notes/${item.id}`);
      }}
    >
      <div
        className={`material-art ${item.kind === "note" ? "purple" : "blue"}`}
      >
        <FileText size={38} />
        <span>{kindLabel(item.kind)}</span>
        <button
          className={`card-save ${item.bookmarked ? "saved" : ""}`}
          disabled={busy}
          aria-label={`${item.bookmarked ? "Убрать из избранного" : "Добавить в избранное"}: ${item.title}`}
          onClick={() =>
            requireUser() &&
            void run(() => api.bookmark(item.id, item.bookmarked))
          }
        >
          <Bookmark
            size={16}
            fill={item.bookmarked ? "currentColor" : "none"}
          />
        </button>
      </div>
      <div className="card-body">
        <div className="card-meta">
          <span className="subject-label purple">
            {item.subject?.title || "Без предмета"}
          </span>
          <span>
            {item.semester ? `${item.semester} семестр` : kindLabel(item.kind)}
          </span>
        </div>
        <button className="card-title" onClick={() => go(`/notes/${item.id}`)}>
          {item.title}
        </button>
        <p>
          {item.snippet ? (
            <SafeSnippet value={item.snippet} />
          ) : (
            item.summary || "Откройте материал, чтобы прочитать конспект."
          )}
        </p>
        <div className="tag-row">
          {item.tags.map((tag) => (
            <span key={tag}>{tag}</span>
          ))}
        </div>
        <button
          className="card-author"
          onClick={() => go(`/authors/${item.author.id}`)}
        >
          <Avatar author={item.author} />
          <span className="live-author-label">
            <strong>{item.author.display_name}</strong>
            {item.author.faculty && <small>{item.author.faculty}</small>}
          </span>
          <time>{formatDate(item.published_at)}</time>
        </button>
        <div className="card-footer">
          <span className="rating">
            <Star size={13} />
            <span>{ratingLabel(item.rating)}</span>
            <small>({item.rating.count})</small>
          </span>
          <span>
            <MessageCircle size={13} />
            {item.comments_count}
          </span>
          <span>{item.views} просмотров</span>
          {item.my_rating !== null && (
            <span className="live-my-rating">
              Ваша оценка: {item.my_rating}
            </span>
          )}
        </div>
      </div>
    </article>
  );
}

export function LibraryFeed({
  saved = false,
  authorId,
  initialSubject = "",
}: {
  saved?: boolean;
  authorId?: string;
  initialSubject?: string;
}) {
  const { user, go, requireUser } = useLive();
  const [search, setSearch] = useState(""),
    [subject, setSubject] = useState(initialSubject),
    [kind, setKind] = useState<"" | Models["MaterialKind"]>(""),
    [semester, setSemester] = useState(""),
    [sort, setSort] = useState<"new" | "rating" | "views">("new"),
    [tag, setTag] = useState("");
  const q = useDebounced(search.trim()),
    exactTag = useDebounced(tag.trim());
  const subjects = useQuery({
    queryKey: ["live", "subjects", user?.id],
    queryFn: api.librarySubjects,
    retry: false,
  });
  const feed = useInfiniteQuery({
    queryKey: [
      "live",
      saved ? "saved" : "library",
      user?.id,
      q,
      subject,
      kind,
      semester,
      sort,
      exactTag,
      authorId,
    ],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      saved
        ? api.bookmarks(pageParam)
        : api.library({
            cursor: pageParam,
            limit: 30,
            q: q || undefined,
            library_subject_id: subject || undefined,
            kind: kind || undefined,
            semester: semester ? Number(semester) : undefined,
            sort,
            tag: exactTag || undefined,
            author_id: authorId,
          }),
    getNextPageParam: (page) => page.next_cursor || undefined,
    enabled: !saved || !!user,
    retry: false,
  });
  if (saved && !user) return <LoginRequired />;
  const items = feed.data?.pages.flatMap((page) => page.items) || [];
  return (
    <>
      <Heading
        title={
          saved
            ? "Избранное"
            : authorId
              ? "Материалы автора"
              : "Библиотека знаний"
        }
        description={
          saved
            ? "Сохранённые материалы — всегда под рукой."
            : "Хорошие конспекты заслуживают большего, чем лежать в папке."
        }
      >
        {!authorId && (
          <button
            className="button primary"
            onClick={() => requireUser() && go("/new")}
          >
            <Plus size={17} />
            Добавить материал
          </button>
        )}
      </Heading>
      {!saved && !authorId && (
        <section className="hero-banner live-hero">
          <div>
            <span className="hero-label">УЧИТЬСЯ ПРОЩЕ ВМЕСТЕ</span>
            <h2>
              Твой следующий «зачёт»
              <br />
              начинается здесь.
            </h2>
            <p>
              Находи понятные конспекты, делись своими знаниями
              <br />и помогай другим разобраться в сложном.
            </p>
          </div>
          <FileText size={110} strokeWidth={1} />
        </section>
      )}
      {!saved && (
        <>
          <div className="filters-row live-filters">
            <label className="search-field">
              <Search size={18} />
              <input
                aria-label="Поиск материалов"
                placeholder="Поиск по названию, теме или тексту…"
                maxLength={200}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </label>
            <select
              aria-label="Сортировка"
              value={sort}
              onChange={(e) => setSort(e.target.value as typeof sort)}
            >
              <option value="new">Сначала новые</option>
              <option value="rating">По оценке</option>
              <option value="views">По просмотрам</option>
            </select>
          </div>
          <div className="live-filter-controls">
            <label>
              Предмет
              <select
                aria-label="Предмет библиотеки"
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
              >
                <option value="">Все предметы</option>
                {subjects.data?.items.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.title}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Тип
              <select
                aria-label="Тип материала"
                value={kind}
                onChange={(e) => setKind(e.target.value as typeof kind)}
              >
                <option value="">Все материалы</option>
                <option value="note">Конспекты</option>
                <option value="cheat_sheet">Шпаргалки</option>
              </select>
            </label>
            <label>
              Семестр
              <select
                aria-label="Семестр"
                value={semester}
                onChange={(e) => setSemester(e.target.value)}
              >
                <option value="">Все семестры</option>
                {Array.from({ length: 12 }, (_, i) => (
                  <option key={i + 1} value={i + 1}>
                    {i + 1}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Тег
              <input
                aria-label="Фильтр по тегу"
                value={tag}
                maxLength={50}
                onChange={(e) => setTag(e.target.value)}
                placeholder="Точное название"
              />
            </label>
          </div>
        </>
      )}
      <ErrorBox error={subjects.error} />
      <ErrorBox error={feed.error} retry={() => void feed.refetch()} />
      {feed.isPending ? (
        <Loading />
      ) : !items.length && !feed.error ? (
        <div className="empty">
          <h2>{saved ? "Здесь пока пусто" : "Пока ничего не нашлось"}</h2>
          <p>
            {saved
              ? "Добавляйте полезные материалы в избранное."
              : "Попробуйте другой запрос или измените фильтры."}
          </p>
        </div>
      ) : (
        <div className="material-grid">
          {items.map((item) => (
            <MaterialCard key={item.id} item={item} />
          ))}
        </div>
      )}
      <NextPage
        hasNext={feed.hasNextPage}
        loading={feed.isFetchingNextPage}
        load={() => void feed.fetchNextPage()}
      />
    </>
  );
}

export function MyNotes() {
  const { user, go } = useLive();
  const notes = useInfiniteQuery({
    queryKey: ["live", "notes", user?.id],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => api.notes({ cursor: pageParam, limit: 30 }),
    getNextPageParam: (page) => page.next_cursor || undefined,
    enabled: !!user,
    retry: false,
  });
  if (!user) return <LoginRequired />;
  return (
    <>
      <Heading
        title="Мои материалы"
        description="Черновики, публикации и решения модераторов."
      >
        <button className="button primary" onClick={() => go("/new")}>
          <Plus size={17} />
          Новый материал
        </button>
      </Heading>
      <ErrorBox error={notes.error} retry={() => void notes.refetch()} />
      {notes.isPending ? (
        <Loading />
      ) : (
        <div className="material-grid">
          {notes.data?.pages
            .flatMap((page) => page.items)
            .map((note) => (
              <article className="aside-panel" key={note.id}>
                <span className={`pill live-status ${note.status}`}>
                  {statuses[note.status]}
                </span>
                <h2 className="live-note-title">{note.title}</h2>
                <p className="muted">
                  {kindLabel(note.kind)} · {formatDate(note.updated_at)}
                </p>
                <div className="live-actions">
                  <button
                    className="button primary"
                    onClick={() => go(`/edit/${note.id}`)}
                  >
                    Редактировать
                  </button>
                  <button
                    className="button secondary"
                    onClick={() => go(`/notes/${note.id}`)}
                  >
                    Открыть
                  </button>
                </div>
              </article>
            ))}
        </div>
      )}
      {notes.data?.pages[0].items.length === 0 && (
        <div className="empty">
          <h2>Создайте свой первый конспект</h2>
          <p>Сохраните его в черновик или отправьте на проверку.</p>
        </div>
      )}
      <NextPage
        hasNext={notes.hasNextPage}
        loading={notes.isFetchingNextPage}
        load={() => void notes.fetchNextPage()}
      />
    </>
  );
}

export function AuthorPage({ id }: { id?: string }) {
  const { user, go } = useLive();
  const ranking = useQuery({
    queryKey: ["live", "ranking", user?.id],
    queryFn: api.ranking,
    enabled: !id,
    retry: false,
  });
  const author = useQuery({
    queryKey: ["live", "author", id, user?.id],
    queryFn: () => api.author(id!),
    enabled: !!id,
    retry: false,
  });
  if (id)
    return (
      <>
        <ErrorBox error={author.error} />
        {author.isPending ? (
          <Loading />
        ) : (
          author.data && (
            <>
              <section className="profile-card">
                <Avatar author={author.data.author} large />
                <div>
                  <h1>{author.data.author.display_name}</h1>
                  <p>{author.data.author.faculty || "Студент StudyVault"}</p>
                  <span className="muted">
                    {author.data.materials_count} материалов ·{" "}
                    {ratingLabel(author.data.rating)} · {author.data.views}{" "}
                    просмотров
                  </span>
                </div>
              </section>
              <LibraryFeed authorId={id} />
            </>
          )
        )}
      </>
    );
  return (
    <>
      <Heading
        title="Рейтинг авторов"
        description="Оценки опубликованных материалов сообщества."
      />
      <ErrorBox error={ranking.error} retry={() => void ranking.refetch()} />
      {ranking.isPending ? (
        <Loading />
      ) : (
        <section className="table-panel">
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Место</th>
                  <th>Автор</th>
                  <th>Материалы</th>
                  <th>Оценка</th>
                  <th>Оценок</th>
                </tr>
              </thead>
              <tbody>
                {ranking.data?.items.map((item) => (
                  <tr key={item.author.id}>
                    <td>{item.rank}</td>
                    <td>
                      <button
                        className="table-user"
                        onClick={() => go(`/authors/${item.author.id}`)}
                      >
                        <Avatar author={item.author} />
                        <span>
                          <strong>{item.author.display_name}</strong>
                          <small>{item.author.faculty}</small>
                        </span>
                      </button>
                    </td>
                    <td>{item.materials_count}</td>
                    <td>{ratingLabel(item.rating)}</td>
                    <td>{item.ratings_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {ranking.data?.items.length === 0 && (
            <p className="muted">Рейтинг появится после первых публикаций.</p>
          )}
        </section>
      )}
    </>
  );
}
