import React, {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import ReactDOM from "react-dom/client";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
  useRouterState,
} from "@tanstack/react-router";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import {
  ArrowDownWideNarrow,
  ArrowLeft,
  ArrowRight,
  Bell,
  Bold,
  BookOpen,
  Bookmark,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Code2,
  Download,
  FileText,
  GraduationCap,
  Grid2X2,
  HelpCircle,
  Italic,
  LayoutList,
  List,
  LogOut,
  Menu,
  MessageCircle,
  Plus,
  Search,
  ShieldCheck,
  Sparkles,
  Star,
  Trophy,
  Upload,
  UserRound,
  Users,
  X,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import {
  average,
  demoUser,
  formatDate,
  repository,
  sessionKey,
  subjectColors,
  subjects,
} from "./data";
import type { Attachment, Database, Note, User } from "./data";
import type { JSONContent } from "@tiptap/react";
import "./styles.css";

const queryClient = new QueryClient();
type AppState = {
  db: Database;
  user: User | null;
  go: (path: string) => void;
  update: (change: (db: Database) => Database) => Promise<boolean>;
  toast: (message: string) => void;
  requireUser: () => User | null;
  selectSubject: (value: string) => void;
};
const AppContext = createContext<AppState>(null!);
const useApp = () => useContext(AppContext);
function Avatar({ user, size = "" }: { user: User; size?: string }) {
  return (
    <span className={`avatar ${user.color} ${size}`}>{user.initials}</span>
  );
}
function IconButton({
  icon: Icon,
  label,
  onClick,
  active = false,
}: {
  icon: LucideIcon;
  label: string;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <button
      type="button"
      className={`icon-button ${active ? "active" : ""}`}
      title={label}
      aria-label={label}
      onClick={onClick}
    >
      <Icon size={18} />
    </button>
  );
}
function Modal({
  title,
  children,
  close,
}: {
  title: string;
  children: React.ReactNode;
  close: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(close);
  closeRef.current = close;
  useEffect(() => {
    const before = document.activeElement as HTMLElement;
    (
      ref.current?.querySelector<HTMLElement>("input,textarea,select") ||
      ref.current?.querySelector<HTMLElement>("button")
    )?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeRef.current();
      if (e.key === "Tab") {
        const els = ref.current?.querySelectorAll<HTMLElement>(
          "button:not(:disabled),input:not(:disabled),textarea,select,a[href]",
        );
        if (!els?.length) return;
        const first = els[0],
          last = els[els.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("keydown", key);
      before?.focus();
    };
  }, []);
  return (
    <div
      className="modal-backdrop"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        className="modal"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        ref={ref}
      >
        <div className="modal-head">
          <h2>{title}</h2>
          <IconButton icon={X} label="Закрыть" onClick={close} />
        </div>
        {children}
      </div>
    </div>
  );
}
function App() {
  const client = useQueryClient();
  const { data: db, error } = useQuery({
    queryKey: ["database"],
    queryFn: repository.read,
  });
  const [userId, setUserId] = useState<string | null>(() => {
    const value = localStorage.getItem(sessionKey);
    return value === null ? demoUser.id : value || null;
  });
  const [subject, setSubject] = useState("Все предметы"),
    [search, setSearch] = useState(""),
    [mobile, setMobile] = useState(false),
    [toast, setToast] = useState(""),
    [dialog, setDialog] = useState<"help" | "search" | "notifications" | null>(
      null,
    );
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const go = (path: string) => {
    void router.navigate({ to: path });
    setMobile(false);
    window.scrollTo(0, 0);
  };
  const user = db?.users.find((u) => u.id === userId) || null;
  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(""), 4000);
    return () => clearTimeout(id);
  }, [toast]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setDialog("search");
      }
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, []);
  const update = async (change: (db: Database) => Database) => {
    try {
      const next = change(client.getQueryData<Database>(["database"])!);
      await repository.write(next);
      client.setQueryData(["database"], next);
      return true;
    } catch {
      setToast(
        "Не удалось сохранить: хранилище браузера заполнено или недоступно. Удалите вложения и повторите.",
      );
      return false;
    }
  };
  const requireUser = () => {
    if (!user) {
      setToast("Войдите, чтобы участвовать в сообществе");
      go("/login");
    }
    return user;
  };
  const login = (id: string | null) => {
    localStorage.setItem(sessionKey, id || "");
    setUserId(id);
    go(id ? "/" : "/login");
  };
  if (error)
    return (
      <div className="loading">
        Не удалось загрузить локальные данные. Проверьте настройки браузера.
      </div>
    );
  if (!db) return <div className="loading">Открываем StudyVault…</div>;
  const state = {
    db,
    user,
    go,
    update,
    toast: setToast,
    requireUser,
    selectSubject: (value: string) => {
      setSubject(value);
      setSearch("");
      go("/");
    },
  };
  if (pathname === "/login" || pathname === "/register")
    return (
      <AppContext.Provider value={state}>
        <Auth register={pathname === "/register"} login={login} />
        {toast && (
          <div className="toast" role="status">
            {toast}
          </div>
        )}
      </AppContext.Provider>
    );
  const pending = db.notes.filter((n) => n.status === "pending").length;
  const navigation: [string, string, LucideIcon][] = [
    ["/", "Библиотека", BookOpen],
    ["/saved", "Избранное", Bookmark],
    ["/my", "Мои материалы", FileText],
    ["/ranking", "Рейтинг студентов", Trophy],
    ["/moderation", "Модерация", ShieldCheck],
  ];
  const currentTitle =
    navigation.find(([path]) => path === pathname)?.[1] ||
    (pathname === "/new"
      ? "Новый материал"
      : pathname === "/profile"
        ? "Мой профиль"
        : "Библиотека");
  return (
    <AppContext.Provider value={state}>
      <div className="app-shell">
        {mobile && (
          <button
            className="sidebar-overlay"
            aria-label="Закрыть меню"
            onClick={() => setMobile(false)}
          />
        )}
        <aside className={`sidebar ${mobile ? "open" : ""}`}>
          <button className="brand" onClick={() => go("/")}>
            <span className="brand-mark">
              <BookOpen size={23} />
            </span>
            study<span>vault</span>
            <span className="brand-dot">.</span>
          </button>
          <span className="brand-caption">ЗНАНИЯ, КОТОРЫМИ ДЕЛЯТСЯ</span>
          <button
            className="sidebar-search"
            onClick={() => setDialog("search")}
          >
            <Search size={16} />
            Быстрый поиск <kbd>⌘ K</kbd>
          </button>
          <nav aria-label="Основная навигация">
            {navigation.map(([path, label, Icon]) => (
              <button
                key={path}
                onClick={() => go(path)}
                className={`nav-item ${pathname === path ? "selected" : ""}`}
              >
                <Icon size={19} />
                {label}
                {path === "/moderation" && pending > 0 && (
                  <span className="nav-count">{pending}</span>
                )}
              </button>
            ))}
          </nav>
          <div className="sidebar-section">
            <span>МОИ ПРЕДМЕТЫ</span>
            <button
              aria-label="Все предметы"
              onClick={() => state.selectSubject("Все предметы")}
            >
              <Plus size={15} />
            </button>
          </div>
          <nav aria-label="Предметы">
            {subjects.slice(1, 6).map((s) => (
              <button
                key={s}
                className={`subject-nav ${subject === s && pathname === "/" ? "selected" : ""}`}
                onClick={() => state.selectSubject(s)}
              >
                <span className={`subject-dot ${subjectColors[s]}`} />
                {s}
                <span className="subject-count">
                  {
                    db.notes.filter(
                      (n) => n.subject === s && n.status === "published",
                    ).length
                  }
                </span>
              </button>
            ))}
          </nav>
          <div className="sidebar-bottom">
            <div className="community-mini">
              <Sparkles size={20} />
              <strong>Знания лучше вместе</strong>
              <p>
                Ваш конспект может помочь
                <br />
                кому-то сдать экзамен.
              </p>
              <button onClick={() => requireUser() && go("/new")}>
                Поделиться материалом <ArrowRight size={14} />
              </button>
            </div>
            <button className="help-button" onClick={() => setDialog("help")}>
              <HelpCircle size={17} />О проекте <span>↗</span>
            </button>
            <div className="demo-badge">
              <span />
              Локальная демоверсия
            </div>
          </div>
        </aside>
        <div className="workspace">
          <header className="topbar">
            <div className="breadcrumb">
              <button
                className="mobile-menu icon-button"
                onClick={() => setMobile(!mobile)}
                aria-label="Открыть меню"
              >
                <Menu size={20} />
              </button>
              <GraduationCap size={19} />
              <span>База знаний</span>
              <ChevronRight size={14} />
              <strong>{currentTitle}</strong>
            </div>
            <div className="topbar-actions">
              <span className="semester-label">Осенний семестр 2026</span>
              <IconButton
                icon={Bell}
                label="Уведомления"
                onClick={() => setDialog("notifications")}
              />
              <span className="topbar-divider" />
              {user ? (
                <button className="user-button" onClick={() => go("/profile")}>
                  <Avatar user={user} />
                  <span>
                    {user.name.split(" ")[0]}
                    <ChevronDown size={14} />
                  </span>
                </button>
              ) : (
                <button
                  className="button primary small"
                  onClick={() => go("/login")}
                >
                  Войти
                </button>
              )}
            </div>
          </header>
          <main className="main-content">
            {pathname === "/" || ["/saved", "/my"].includes(pathname) ? (
              <Library
                mode={pathname}
                subject={subject}
                setSubject={setSubject}
                search={search}
                setSearch={setSearch}
              />
            ) : pathname.startsWith("/notes/") ? (
              <NotePage
                key={pathname}
                id={decodeURIComponent(pathname.slice(7))}
              />
            ) : pathname === "/new" || pathname.startsWith("/edit/") ? (
              <NoteEditor
                key={pathname}
                id={
                  pathname.startsWith("/edit/")
                    ? decodeURIComponent(pathname.slice(6))
                    : undefined
                }
              />
            ) : pathname === "/ranking" ? (
              <Ranking />
            ) : pathname === "/moderation" ? (
              <Moderation />
            ) : pathname === "/profile" ? (
              <Profile logout={() => login(null)} />
            ) : (
              <div className="empty">
                <BookOpen />
                <h2>Страница не найдена</h2>
                <button className="button primary" onClick={() => go("/")}>
                  В библиотеку
                </button>
              </div>
            )}
          </main>
          <footer className="page-footer">
            <span>StudyVault © 2026</span>
            <span>
              От студентов — студентам <span className="tiny-heart">♡</span>
            </span>
          </footer>
        </div>
      </div>
      {toast && (
        <div className="toast" role="status">
          <CheckCircle2 size={19} />
          {toast}
        </div>
      )}
      {dialog === "help" && (
        <Modal title="Учиться проще вместе" close={() => setDialog(null)}>
          <p>
            StudyVault — база знаний студентов: конспекты, шпаргалки, обсуждения
            и полезные материалы в одном месте.
          </p>
          <div className="info-box">
            Это локальная демоверсия. Данные сохраняются только в этом браузере.
            Вход, регистрация и роль модератора моделируются; реальные аккаунты
            и сервер пока не подключены. Используйте вымышленные данные.
          </div>
          <p>
            Нажмите <kbd>Ctrl K</kbd>, чтобы быстро найти материал. Создайте
            конспект и отправьте его на проверку во вкладке «Модерация».
          </p>
        </Modal>
      )}
      {dialog === "notifications" && (
        <Modal title="Уведомления" close={() => setDialog(null)}>
          <div className="notification-item">
            <ShieldCheck />
            <div>
              <strong>Материалы ждут проверки</strong>
              <p>
                В демоочереди: {pending}. Здесь можно показать работу
                модератора.
              </p>
              <button
                className="text-button"
                onClick={() => {
                  setDialog(null);
                  go("/moderation");
                }}
              >
                Открыть модерацию →
              </button>
            </div>
          </div>
        </Modal>
      )}
      {dialog === "search" && (
        <Modal title="Найти в StudyVault" close={() => setDialog(null)}>
          <div className="search-field">
            <Search size={19} />
            <input
              autoFocus
              aria-label="Быстрый поиск"
              placeholder="Название, тема или тег…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="quick-results">
            {db.notes
              .filter(
                (n) =>
                  n.status === "published" &&
                  `${n.title} ${n.tags.join(" ")} ${n.subject} ${n.text}`
                    .toLowerCase()
                    .includes(search.toLowerCase()),
              )
              .slice(0, 6)
              .map((n) => (
                <button
                  key={n.id}
                  onClick={() => {
                    setDialog(null);
                    go(`/notes/${n.id}`);
                  }}
                >
                  <FileText size={18} />
                  <span>
                    {n.title}
                    <small>{n.subject}</small>
                  </span>
                  <ArrowRight size={16} />
                </button>
              ))}
          </div>
        </Modal>
      )}
    </AppContext.Provider>
  );
}
function MaterialArt({ subject, type }: { subject: string; type: string }) {
  return (
    <div
      className={`material-art ${subjectColors[subject] || "purple"}`}
      aria-hidden="true"
    >
      <span className="art-grid" />
      <span className="art-type">
        {type === "Шпаргалка" ? "КРАТКО И ПО ДЕЛУ" : "РАЗБИРАЕМ ПО ПОЛОЧКАМ"}
      </span>
      {subject === "Математика" ? (
        <>
          <span className="math-symbol">ƒ′(x)</span>
          <svg className="curve" viewBox="0 0 150 90">
            <path d="M10 76H140M38 90V5" />
            <path d="M14 68Q58 90 78 41T140 12" />
            <path className="dashed" d="M33 80L133 7" />
          </svg>
          <span className="art-caption">МАТЕМАТИКА / 02</span>
        </>
      ) : subject === "Базы данных" ? (
        <>
          <span className="code-art">
            SELECT knowledge
            <br />
            <span>FROM studyvault</span>
            <br />
            WHERE curiosity = true;
          </span>
          <span className="art-caption">ЗАПРОСЫ, КОТОРЫЕ ПОНЯТНЫ</span>
          <span className="art-orbit">↗</span>
        </>
      ) : subject === "Программирование" ? (
        <>
          <span className="algorithm-bars">
            {[25, 45, 35, 65, 53, 83].map((v, i) => (
              <i key={i} style={{ height: v }} />
            ))}
          </span>
          <span className="art-code">{"{ }"}</span>
          <span className="art-caption">ОТ ИДЕИ К РЕШЕНИЮ</span>
        </>
      ) : subject === "Физика" ? (
        <>
          <span className="physics-art">
            E = mc<sup>2</sup>
          </span>
          <span className="atom">
            <i />
            <i />
            <i />
            <b />
          </span>
          <span className="art-caption">ВСЁ ПОДЧИНЯЕТСЯ ЗАКОНАМ</span>
        </>
      ) : (
        <>
          <span className="language-art">
            Past.
            <br />
            Present. <span>Future.</span>
          </span>
          <span className="art-caption">A LITTLE PRACTICE, EVERY DAY</span>
          <span className="art-orbit">Aa</span>
        </>
      )}
    </div>
  );
}
function Library({
  mode,
  subject,
  setSubject,
  search,
  setSearch,
}: {
  mode: string;
  subject: string;
  setSubject: (s: string) => void;
  search: string;
  setSearch: (s: string) => void;
}) {
  const { db, user, go, requireUser } = useApp();
  const [type, setType] = useState("Все материалы"),
    [sort, setSort] = useState("popular"),
    [layout, setLayout] = useState("grid"),
    [semester, setSemester] = useState("all");
  const published = db.notes.filter((n) => n.status === "published"),
    isLibrary = mode === "/";
  const notes = db.notes
    .filter(
      (n) =>
        (mode === "/my"
          ? !!user && n.authorId === user.id
          : n.status === "published" &&
            (mode !== "/saved" || (!!user && n.saves.includes(user.id)))) &&
        (subject === "Все предметы" || n.subject === subject || !isLibrary) &&
        (type === "Все материалы" || n.type === type) &&
        (semester === "all" || n.semester === Number(semester)) &&
        `${n.title} ${n.description} ${n.tags.join(" ")} ${n.subject} ${n.text}`
          .toLowerCase()
          .includes(search.toLowerCase()),
    )
    .sort((a, b) =>
      sort === "new"
        ? b.createdAt.localeCompare(a.createdAt)
        : sort === "rating"
          ? average(b) - average(a)
          : b.views - a.views,
    );
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow">
            <span />
            ВАША СТУДЕНЧЕСКАЯ СУПЕРСИЛА
          </div>
          <h1>
            {isLibrary
              ? "Библиотека знаний"
              : mode === "/saved"
                ? "Всегда под рукой"
                : "Мои материалы"}
            <span className="heading-dot">.</span>
          </h1>
          <p>
            {isLibrary
              ? "Хорошие конспекты заслуживают большего, чем лежать в папке."
              : mode === "/saved"
                ? "Конспекты и шпаргалки, которые вы сохранили для себя."
                : "Ваш вклад в общую базу знаний. Делитесь тем, что знаете."}
          </p>
        </div>
        <button
          className="button primary"
          onClick={() => requireUser() && go("/new")}
        >
          <Plus size={18} />
          Добавить материал
        </button>
      </div>
      {isLibrary && (
        <>
          <section className="hero-banner">
            <div>
              <span className="hero-label">
                <Sparkles size={15} />
                УЧИТЬСЯ ПРОЩЕ ВМЕСТЕ
              </span>
              <h2>
                Твой следующий «зачёт»
                <br />
                начинается здесь.
              </h2>
              <p>
                Находи понятные конспекты, делись своими
                <br />и помогай другим разобраться в сложном.
              </p>
              <button
                onClick={() => {
                  setSort("rating");
                  document
                    .getElementById("materials")
                    ?.scrollIntoView({ behavior: "smooth" });
                }}
              >
                Найти что-то полезное <ArrowRight size={17} />
              </button>
            </div>
            <div className="hero-illustration" aria-hidden="true">
              <span className="spark spark-one">✦</span>
              <span className="spark spark-two">✧</span>
              <span className="hero-loop" />
              <div className="floating-note back">
                <span />
                <span />
                <span />
                <span />
              </div>
              <div className="floating-note front">
                <div className="note-top">
                  <span className="note-mini-icon">
                    <BookOpen size={19} />
                  </span>
                  <span>
                    Конспект, который
                    <br />
                    <strong>всё объясняет</strong>
                  </span>
                  <span className="check-sticker">
                    <Check size={17} />
                  </span>
                </div>
                <div className="note-lines">
                  <i />
                  <i />
                  <i />
                </div>
                <div className="note-formula">
                  знания + обмен = <span>рост ↗</span>
                </div>
                <div className="note-bottom">
                  <span className="stacked-avatars">
                    <i>А</i>
                    <i>М</i>
                    <i>Д</i>
                  </span>
                  <span>Сохранили одногруппники</span>
                </div>
              </div>
              <span className="floating-label">
                <Star size={15} fill="currentColor" />
                5.0 · Очень полезно
              </span>
            </div>
          </section>
          <div className="stats-strip">
            <div>
              <span className="stat-icon purple">
                <FileText size={19} />
              </span>
              <strong>
                {published.length}
                <small>материалов в библиотеке</small>
              </strong>
            </div>
            <div>
              <span className="stat-icon blue">
                <Users size={19} />
              </span>
              <strong>
                {db.users.length}
                <small>студентов делятся знаниями</small>
              </strong>
            </div>
            <div>
              <span className="stat-icon orange">
                <BookOpen size={19} />
              </span>
              <strong>
                {new Set(published.map((n) => n.subject)).size}
                <small>учебных предметов</small>
              </strong>
            </div>
            <span className="stats-note">
              <span />
              Каждый конспект — чей-то «спасибо»
            </span>
          </div>
        </>
      )}
      <section id="materials">
        <div className="section-heading">
          <h2>
            {isLibrary ? "Найди то, что нужно" : "Ваша подборка"}{" "}
            <span>{notes.length}</span>
          </h2>
          <label className="sort-control">
            <ArrowDownWideNarrow size={16} />
            <select
              aria-label="Сортировка"
              value={sort}
              onChange={(e) => setSort(e.target.value)}
            >
              <option value="popular">Сначала популярные</option>
              <option value="new">Сначала новые</option>
              <option value="rating">По оценке</option>
            </select>
          </label>
        </div>
        <div className="filters-row">
          <div className="search-field">
            <Search size={19} />
            <input
              placeholder="Поиск по названию, теме или тегу…"
              aria-label="Поиск материалов"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {search && (
              <IconButton
                icon={X}
                label="Очистить поиск"
                onClick={() => setSearch("")}
              />
            )}
          </div>
          <select
            className="filter-select"
            aria-label="Семестр"
            value={semester}
            onChange={(e) => setSemester(e.target.value)}
          >
            <option value="all">Все семестры</option>
            {[1, 2, 3, 4, 5, 6, 7, 8].map((n) => (
              <option key={n} value={n}>
                {n} семестр
              </option>
            ))}
          </select>
          <div className="layout-toggle">
            <IconButton
              icon={Grid2X2}
              label="Карточки"
              active={layout === "grid"}
              onClick={() => setLayout("grid")}
            />
            <IconButton
              icon={LayoutList}
              label="Список"
              active={layout === "list"}
              onClick={() => setLayout("list")}
            />
          </div>
        </div>
        {isLibrary && (
          <div className="subject-chips">
            {subjects.slice(0, 6).map((s) => (
              <button
                className={subject === s ? "active" : ""}
                key={s}
                onClick={() => setSubject(s)}
              >
                {s === "Все предметы" ? (
                  <Grid2X2 size={14} />
                ) : (
                  <span className={`subject-dot ${subjectColors[s]}`} />
                )}{" "}
                {s}
              </button>
            ))}
          </div>
        )}
        <div className="material-tabs">
          {["Все материалы", "Конспект", "Шпаргалка"].map((t) => (
            <button
              className={type === t ? "active" : ""}
              key={t}
              onClick={() => setType(t)}
            >
              {t === "Конспект"
                ? "Конспекты"
                : t === "Шпаргалка"
                  ? "Шпаргалки"
                  : t}
            </button>
          ))}
          <span>
            Проверено сообществом <ShieldCheck size={14} />
          </span>
        </div>
        {notes.length ? (
          <div
            className={`material-grid ${layout === "list" ? "list-layout" : ""}`}
          >
            {notes.map((n) => (
              <NoteCard key={n.id} note={n} />
            ))}
          </div>
        ) : (
          <div className="empty">
            <Search size={35} />
            <h2>
              {mode === "/saved" && !search
                ? "Здесь будут ваши находки"
                : "Пока ничего не нашлось"}
            </h2>
            <p>
              {mode === "/saved"
                ? "Нажмите на закладку у материала, чтобы сохранить его."
                : "Попробуйте другой запрос или уберите фильтры."}
            </p>
            <button
              className="button secondary"
              onClick={() => {
                setSearch("");
                setSubject("Все предметы");
                setType("Все материалы");
                setSemester("all");
                if (mode !== "/") go("/");
              }}
            >
              Открыть все материалы
            </button>
          </div>
        )}
      </section>
    </>
  );
}
function NoteCard({ note }: { note: Note }) {
  const { db, user, go, update, requireUser } = useApp(),
    author = db.users.find((u) => u.id === note.authorId)!,
    saved = !!user && note.saves.includes(user.id);
  const save = async () => {
    const u = requireUser();
    if (!u) return;
    await update((db) => ({
      ...db,
      notes: db.notes.map((n) =>
        n.id === note.id
          ? {
              ...n,
              saves: saved
                ? n.saves.filter((id) => id !== u.id)
                : [...n.saves, u.id],
            }
          : n,
      ),
    }));
  };
  return (
    <article className="material-card">
      <button
        className="art-button"
        tabIndex={-1}
        aria-label={`Открыть ${note.title}`}
        onClick={() => go(`/notes/${note.id}`)}
      >
        <MaterialArt subject={note.subject} type={note.type} />
      </button>
      <button
        className={`card-save ${saved ? "saved" : ""}`}
        aria-label={`${saved ? "Убрать из" : "Добавить в"} избранное: ${note.title}`}
        onClick={save}
      >
        <Bookmark size={16} fill={saved ? "currentColor" : "none"} />
      </button>
      <div className="card-body">
        <div className="card-meta">
          <span className={`subject-label ${subjectColors[note.subject]}`}>
            {note.subject}
          </span>
          <span>
            {note.type === "Шпаргалка"
              ? "Шпаргалка"
              : `${note.semester} семестр`}
          </span>
        </div>
        <button className="card-title" onClick={() => go(`/notes/${note.id}`)}>
          {note.title}
        </button>
        <p>{note.description}</p>
        <div className="tag-row">
          {note.tags.slice(0, 2).map((tag) => (
            <span key={tag}>#{tag}</span>
          ))}
          {note.tags.length > 2 && <span>+{note.tags.length - 2}</span>}
        </div>
        <div className="card-author">
          <Avatar user={author} size="tiny" />
          <span>{author.name}</span>
          <time>{formatDate(note.createdAt)}</time>
        </div>
        <div className="card-footer">
          <span className="rating">
            <Star size={14} fill="currentColor" />
            {average(note) ? average(note).toFixed(1) : "—"}
            <small>({Object.keys(note.ratings).length})</small>
          </span>
          <span>
            <MessageCircle size={14} />
            {note.comments.length}
          </span>
          <span>
            <BookOpen size={14} />
            {note.views}
          </span>
          {note.status !== "published" && (
            <span className={`status-label ${note.status}`}>
              {statusText[note.status]}
            </span>
          )}
          {note.status === "published" && (
            <span className="verified" title="Материал проверен">
              <ShieldCheck size={15} />
            </span>
          )}
        </div>
      </div>
    </article>
  );
}
const statusText = {
  published: "Опубликован",
  pending: "На проверке",
  rejected: "Отклонён",
  draft: "Черновик",
};
function RenderContent({ node }: { node: JSONContent }) {
  if (node.type === "text") {
    let result: React.ReactNode = node.text;
    node.marks?.forEach((m) => {
      if (m.type === "bold") result = <strong>{result}</strong>;
      if (m.type === "italic") result = <em>{result}</em>;
      if (m.type === "code") result = <code>{result}</code>;
    });
    return <>{result}</>;
  }
  const children = node.content?.map((n, i) => (
    <RenderContent key={i} node={n} />
  ));
  switch (node.type) {
    case "heading":
      return node.attrs?.level === 3 ? (
        <h3>{children}</h3>
      ) : (
        <h2>{children}</h2>
      );
    case "paragraph":
      return <p>{children}</p>;
    case "blockquote":
      return <blockquote>{children}</blockquote>;
    case "bulletList":
      return <ul>{children}</ul>;
    case "orderedList":
      return <ol>{children}</ol>;
    case "listItem":
      return <li>{children}</li>;
    case "codeBlock":
      return (
        <pre>
          <code>{children}</code>
        </pre>
      );
    case "hardBreak":
      return <br />;
    case "horizontalRule":
      return <hr />;
    default:
      return <>{children}</>;
  }
}
function NotePage({ id }: { id: string }) {
  const { db, user, go, update, requireUser, toast } = useApp(),
    note = db.notes.find((n) => n.id === id);
  const [comment, setComment] = useState("");
  if (!note)
    return (
      <div className="empty">
        <h2>Материал не найден</h2>
        <button className="button primary" onClick={() => go("/")}>
          В библиотеку
        </button>
      </div>
    );
  if (
    note.status !== "published" &&
    note.authorId !== user?.id &&
    user?.id !== demoUser.id
  )
    return (
      <div className="empty">
        <ShieldCheck />
        <h2>Материал ещё не опубликован</h2>
        <button className="button secondary" onClick={() => go("/")}>
          В библиотеку
        </button>
      </div>
    );
  const author = db.users.find((u) => u.id === note.authorId)!,
    saved = !!user && note.saves.includes(user.id);
  const mutate = (change: (n: Note) => Note) =>
    update((db) => ({
      ...db,
      notes: db.notes.map((n) => (n.id === id ? change(n) : n)),
    }));
  return (
    <>
      <button className="back-link" onClick={() => go("/")}>
        <ArrowLeft size={16} />
        Назад в библиотеку
      </button>
      <div className="reader-layout">
        <article className="reader">
          {note.authorId === user?.id && (
            <button
              className="button secondary small"
              style={{ float: "right" }}
              onClick={() => go(`/edit/${note.id}`)}
            >
              Редактировать
            </button>
          )}
          <div className="reader-meta">
            <span className={`subject-label ${subjectColors[note.subject]}`}>
              {note.subject}
            </span>
            <span>
              {note.type} · {note.semester} семестр
            </span>
          </div>
          <h1>{note.title}</h1>
          <p className="reader-description">{note.description}</p>
          <div className="reader-author">
            <Avatar user={author} />
            <div>
              <strong>{author.name}</strong>
              <span>
                {formatDate(note.createdAt)} ·{" "}
                {Math.max(2, Math.ceil(note.text.length / 1000))} мин. чтения
              </span>
            </div>
            <span className="reader-verified">
              <ShieldCheck size={16} />
              {statusText[note.status]}
            </span>
          </div>
          {note.rejection && (
            <div className="info-box">Причина отклонения: {note.rejection}</div>
          )}
          <div className="prose">
            <RenderContent node={note.content} />
          </div>
          <div className="tag-row reader-tags">
            {note.tags.map((t) => (
              <span key={t}>#{t}</span>
            ))}
          </div>
          {note.attachments.length > 0 && (
            <div className="attachments">
              <h3>Вложения</h3>
              {note.attachments.map((a, i) => (
                <a
                  key={i}
                  className="attachment"
                  href={a.dataUrl}
                  download={a.name}
                >
                  <FileText size={22} />
                  <span>
                    {a.name}
                    <small>{(a.size / 1024).toFixed(0)} КБ</small>
                  </span>
                  <Download size={18} />
                </a>
              ))}
            </div>
          )}
          <section className="comments">
            <h2>
              Обсуждение <span>{note.comments.length}</span>
            </h2>
            <p className="muted">
              Задайте вопрос, дополните материал или поблагодарите автора.
            </p>
            <form
              onSubmit={async (e) => {
                e.preventDefault();
                const u = requireUser();
                if (!u || !comment.trim()) return;
                if (
                  await mutate((n) => ({
                    ...n,
                    comments: [
                      ...n.comments,
                      {
                        id: crypto.randomUUID(),
                        userId: u.id,
                        text: comment.trim(),
                        createdAt: new Date().toISOString(),
                      },
                    ],
                  }))
                ) {
                  setComment("");
                  toast("Комментарий добавлен");
                }
              }}
            >
              <textarea
                aria-label="Комментарий"
                maxLength={2000}
                placeholder="Что думаете об этом материале?"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
              <button
                className="button primary small"
                disabled={!comment.trim()}
              >
                Отправить комментарий <ArrowRight size={15} />
              </button>
            </form>
            {note.comments.map((c) => {
              const u = db.users.find((u) => u.id === c.userId)!;
              return (
                <div className="comment" key={c.id}>
                  <Avatar user={u} />
                  <div>
                    <strong>{u.name}</strong>
                    <time>{formatDate(c.createdAt)}</time>
                    <p>{c.text}</p>
                  </div>
                </div>
              );
            })}
          </section>
        </article>
        <aside className="reader-aside">
          <div className="aside-panel">
            <div className="big-rating">
              <Star fill="currentColor" size={23} />
              <strong>{average(note) ? average(note).toFixed(1) : "—"}</strong>
              <span>Оценка сообщества</span>
            </div>
            <p>Было полезно? Оцените материал</p>
            <div className="star-picker">
              {[1, 2, 3, 4, 5].map((v) => (
                <button
                  key={v}
                  aria-label={`Оценить на ${v}`}
                  title={`${v} из 5`}
                  onClick={async () => {
                    const u = requireUser();
                    if (
                      u &&
                      (await mutate((n) => ({
                        ...n,
                        ratings: { ...n.ratings, [u.id]: v },
                      })))
                    )
                      toast("Ваша оценка сохранена");
                  }}
                >
                  <Star
                    size={25}
                    fill={
                      user && (note.ratings[user.id] || 0) >= v
                        ? "currentColor"
                        : "none"
                    }
                  />
                </button>
              ))}
            </div>
            <button
              className={`button ${saved ? "primary" : "secondary"} full`}
              onClick={() => {
                const u = requireUser();
                if (u)
                  void mutate((n) => ({
                    ...n,
                    saves: saved
                      ? n.saves.filter((id) => id !== u.id)
                      : [...n.saves, u.id],
                  }));
              }}
            >
              <Bookmark size={17} />
              {saved ? "В избранном" : "Сохранить материал"}
            </button>
          </div>
          <div className="aside-panel author-panel">
            <Avatar user={author} size="large" />
            <h3>{author.name}</h3>
            <p>{author.faculty}</p>
            <span>
              <Trophy size={16} />
              {
                db.notes.filter(
                  (n) => n.authorId === author.id && n.status === "published",
                ).length
              }{" "}
              опубликованных материала
            </span>
          </div>
          <div className="aside-panel">
            <h3>По этой теме</h3>
            {db.notes
              .filter(
                (n) =>
                  n.id !== id &&
                  n.status === "published" &&
                  n.subject === note.subject,
              )
              .slice(0, 3)
              .map((n) => (
                <button
                  className="related-note"
                  key={n.id}
                  onClick={() => go(`/notes/${n.id}`)}
                >
                  <FileText size={17} />
                  {n.title}
                  <ChevronRight size={15} />
                </button>
              ))}
          </div>
        </aside>
      </div>
    </>
  );
}
function NoteEditor({ id }: { id?: string }) {
  const { db, user, go, update, toast } = useApp();
  const original = db.notes.find((n) => n.id === id && n.authorId === user?.id);
  const [title, setTitle] = useState(original?.title || ""),
    [description, setDescription] = useState(original?.description || ""),
    [subject, setSubject] = useState(original?.subject || "Математика"),
    [type, setType] = useState<Note["type"]>(original?.type || "Конспект"),
    [semester, setSemester] = useState(original?.semester || 2),
    [tags, setTags] = useState(original?.tags.join(", ") || ""),
    [attachments, setAttachments] = useState<Attachment[]>(
      original?.attachments || [],
    ),
    [busy, setBusy] = useState(false),
    [fileError, setFileError] = useState("");
  const editor = useEditor({
    extensions: [StarterKit.configure({ heading: { levels: [2, 3] } })],
    content: original?.content || {
      type: "doc",
      content: [{ type: "paragraph" }],
    },
    editorProps: {
      attributes: {
        class: "prose editor-prose",
        "aria-label": "Текст материала",
        role: "textbox",
      },
    },
  });
  if (!user)
    return (
      <div className="empty">
        <UserRound />
        <h2>Войдите, чтобы поделиться знаниями</h2>
        <button className="button primary" onClick={() => go("/login")}>
          Войти
        </button>
      </div>
    );
  if (id && !original)
    return (
      <div className="empty">
        <h2>Этот материал недоступен для редактирования</h2>
        <button className="button secondary" onClick={() => go("/my")}>
          Мои материалы
        </button>
      </div>
    );
  const save = async (status: Note["status"]) => {
    if (!title.trim() || !editor?.getText().trim()) {
      toast("Добавьте название и текст материала");
      return;
    }
    if (busy) return;
    setBusy(true);
    const n: Note = {
      id: original?.id || crypto.randomUUID(),
      title: title.trim(),
      description: description.trim() || editor.getText().slice(0, 160),
      subject,
      type,
      semester,
      tags: [
        ...new Set(
          tags
            .split(",")
            .map((t) => t.trim().replace(/^#/, ""))
            .filter(Boolean),
        ),
      ].slice(0, 8),
      authorId: user.id,
      createdAt: original?.createdAt || new Date().toISOString(),
      content: editor.getJSON(),
      text: editor.getText(),
      status,
      ratings: original?.ratings || {},
      saves: original?.saves || [],
      comments: original?.comments || [],
      attachments,
      views: original?.views || 0,
    };
    const ok = await update((db) => ({
      ...db,
      notes: original
        ? db.notes.map((old) => (old.id === n.id ? n : old))
        : [n, ...db.notes],
    }));
    setBusy(false);
    if (ok) {
      toast(
        status === "draft"
          ? "Черновик сохранён"
          : "Материал отправлен на модерацию",
      );
      go("/my");
    }
  };
  return (
    <>
      <button className="back-link" onClick={() => go("/my")}>
        <ArrowLeft size={16} />
        Мои материалы
      </button>
      <div className="page-heading">
        <div>
          <div className="eyebrow">ДЕЛИТЕСЬ ТЕМ, ЧТО ЗНАЕТЕ</div>
          <h1>
            {original ? "Редактировать материал" : "Новый материал"}
            <span className="heading-dot">.</span>
          </h1>
          <p>Один хороший конспект может помочь целой группе.</p>
        </div>
      </div>
      <div className="editor-layout">
        <section className="editor-card">
          <label className="field-label">
            Название материала
            <input
              className="title-input"
              maxLength={120}
              placeholder="Например, интегралы простыми словами"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </label>
          <label className="field-label">
            Краткое описание
            <textarea
              maxLength={300}
              placeholder="О чём этот материал и кому он пригодится?"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </label>
          <label className="field-label">Содержание</label>
          <div className="editor-toolbar">
            <IconButton
              icon={Bold}
              label="Полужирный"
              onClick={() => editor?.chain().focus().toggleBold().run()}
            />
            <IconButton
              icon={Italic}
              label="Курсив"
              onClick={() => editor?.chain().focus().toggleItalic().run()}
            />
            <button
              type="button"
              onClick={() =>
                editor?.chain().focus().toggleHeading({ level: 2 }).run()
              }
              aria-label="Заголовок"
            >
              H₂
            </button>
            <IconButton
              icon={List}
              label="Маркированный список"
              onClick={() => editor?.chain().focus().toggleBulletList().run()}
            />
            <IconButton
              icon={Code2}
              label="Блок кода"
              onClick={() => editor?.chain().focus().toggleCodeBlock().run()}
            />
          </div>
          <EditorContent editor={editor} />
          <p className="editor-hint">
            Начните с главного. Добавьте определения, примеры и понятные
            пояснения.
          </p>
          <label className="upload-zone">
            <Upload size={24} />
            <strong>Добавить файлы к конспекту</strong>
            <span>PDF или изображения · до 2 МБ · максимум 3 файла</span>
            <input
              type="file"
              accept="application/pdf,image/png,image/jpeg,image/webp"
              disabled={busy}
              onChange={async (e) => {
                const file = e.target.files?.[0];
                e.target.value = "";
                if (!file) return;
                if (
                  file.size > 2 * 1024 * 1024 ||
                  attachments.length >= 3 ||
                  ![
                    "application/pdf",
                    "image/png",
                    "image/jpeg",
                    "image/webp",
                  ].includes(file.type)
                ) {
                  setFileError(
                    "Выберите PDF или изображение до 2 МБ. Не более трёх файлов.",
                  );
                  return;
                }
                setBusy(true);
                try {
                  const dataUrl = await new Promise<string>(
                    (resolve, reject) => {
                      const r = new FileReader();
                      r.onload = () => resolve(String(r.result));
                      r.onerror = reject;
                      r.readAsDataURL(file);
                    },
                  );
                  setAttachments((a) => [
                    ...a,
                    { name: file.name, size: file.size, dataUrl },
                  ]);
                  setFileError("");
                } catch {
                  setFileError("Не удалось прочитать файл");
                } finally {
                  setBusy(false);
                }
              }}
            />
          </label>
          {fileError && (
            <p className="form-error" role="alert">
              {fileError}
            </p>
          )}
          {attachments.map((a, i) => (
            <div className="attachment" key={i}>
              <FileText size={20} />
              <span>{a.name}</span>
              <IconButton
                icon={X}
                label={`Удалить ${a.name}`}
                onClick={() =>
                  setAttachments((a) => a.filter((_, idx) => idx !== i))
                }
              />
            </div>
          ))}
        </section>
        <aside>
          <div className="aside-panel editor-settings">
            <h3>О материале</h3>
            <label className="field-label">
              Предмет
              <select
                value={subject}
                onChange={(e) => setSubject(e.target.value)}
              >
                {subjects.slice(1).map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
            </label>
            <label className="field-label">
              Тип материала
              <select
                value={type}
                onChange={(e) => setType(e.target.value as Note["type"])}
              >
                <option>Конспект</option>
                <option>Шпаргалка</option>
              </select>
            </label>
            <label className="field-label">
              Семестр
              <select
                value={semester}
                onChange={(e) => setSemester(Number(e.target.value))}
              >
                {[1, 2, 3, 4, 5, 6, 7, 8].map((n) => (
                  <option key={n} value={n}>
                    {n} семестр
                  </option>
                ))}
              </select>
            </label>
            <label className="field-label">
              Теги
              <input
                value={tags}
                maxLength={160}
                placeholder="матан, интегралы, экзамен"
                onChange={(e) => setTags(e.target.value)}
              />
              <small>Через запятую, до 8 тегов</small>
            </label>
            <button
              className="button primary full"
              disabled={busy}
              onClick={() => save("pending")}
            >
              <Upload size={16} />
              {busy ? "Сохраняем…" : "Отправить на проверку"}
            </button>
            <button
              className="button secondary full"
              disabled={busy}
              onClick={() => save("draft")}
            >
              Сохранить черновик
            </button>
            <p className="muted small-text">
              <ShieldCheck size={15} />
              После проверки материал появится в общей библиотеке.
            </p>
          </div>
          <div className="writing-tip">
            <Sparkles size={20} />
            <h3>Сделайте понятно</h3>
            <p>
              Пишите своими словами, указывайте источники и проверяйте формулы.
              Ваши одногруппники скажут спасибо.
            </p>
          </div>
        </aside>
      </div>
    </>
  );
}
function Ranking() {
  const { db } = useApp();
  const ranks = db.users
    .map((u) => {
      const notes = db.notes.filter(
        (n) => n.authorId === u.id && n.status === "published",
      );
      return {
        user: u,
        notes: notes.length,
        score: notes.reduce(
          (s, n) =>
            s +
            n.saves.length * 10 +
            n.comments.length * 5 +
            Object.keys(n.ratings).length * 3,
          0,
        ),
        rating: notes.length
          ? notes.reduce((s, n) => s + average(n), 0) / notes.length
          : 0,
      };
    })
    .sort((a, b) => b.score - a.score);
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow">ЛЮДИ, КОТОРЫЕ ПОМОГАЮТ</div>
          <h1>
            Знания создают люди<span className="heading-dot">.</span>
          </h1>
          <p>Спасибо тем, кто делится и делает сложное понятным.</p>
        </div>
        <span className="pill">
          <Trophy size={16} />
          Рейтинг сообщества
        </span>
      </div>
      <div className="podium">
        {ranks.slice(0, 3).map((r, i) => (
          <div key={r.user.id} className={`podium-card place-${i}`}>
            <span className="place-badge">
              <Trophy size={18} />
              {i + 1} место
            </span>
            <Avatar user={r.user} size="large" />
            <h2>{r.user.name}</h2>
            <p>{r.user.faculty}</p>
            <strong>
              {r.score}
              <small>баллов вклада</small>
            </strong>
            <div>
              {r.notes} материала <span>·</span>
              <Star size={14} fill="currentColor" />
              {r.rating.toFixed(1)}
            </div>
          </div>
        ))}
      </div>
      <div className="table-panel">
        <div className="section-heading">
          <h2>Рейтинг студентов</h2>
          <span className="muted">Считаем полезный вклад, а не количество</span>
        </div>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Место</th>
                <th>Студент</th>
                <th>Материалы</th>
                <th>Средняя оценка</th>
                <th>Баллы</th>
              </tr>
            </thead>
            <tbody>
              {ranks.map((r, i) => (
                <tr key={r.user.id}>
                  <td>
                    <span className="rank-number">
                      {String(i + 1).padStart(2, "0")}
                    </span>
                  </td>
                  <td>
                    <div className="table-user">
                      <Avatar user={r.user} />
                      <span>
                        <strong>{r.user.name}</strong>
                        <small>{r.user.faculty}</small>
                      </span>
                    </div>
                  </td>
                  <td>{r.notes}</td>
                  <td>
                    <span className="rating">
                      <Star size={14} fill="currentColor" />
                      {r.rating ? r.rating.toFixed(1) : "—"}
                    </span>
                  </td>
                  <td className="score">{r.score}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <p className="rating-explanation">
        Как считаются баллы: сохранение материала — 10, комментарий — 5, оценка
        — 3. В расчёте участвуют опубликованные материалы и демонстрационные
        оценки.
      </p>
    </>
  );
}
function Moderation() {
  const { db, user, go, update, toast } = useApp(),
    [tab, setTab] = useState("pending"),
    [rejectId, setRejectId] = useState<string | null>(null),
    [reason, setReason] = useState("");
  if (user?.id !== demoUser.id)
    return (
      <div className="empty">
        <ShieldCheck size={35} />
        <h2>Раздел модератора</h2>
        <p>Для демонстрации войдите в демоаккаунт Алексея.</p>
        <button className="button primary" onClick={() => go("/login")}>
          Открыть вход
        </button>
      </div>
    );
  const moderate = async (
    id: string,
    status: Note["status"],
    rejection?: string,
  ) => {
    if (
      await update((db) => ({
        ...db,
        notes: db.notes.map((n) =>
          n.id === id ? { ...n, status, rejection } : n,
        ),
      }))
    ) {
      toast(
        status === "published"
          ? "Материал опубликован в библиотеке"
          : "Материал отклонён, причина сохранена",
      );
      setRejectId(null);
      setReason("");
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow">КАЧЕСТВО НАЧИНАЕТСЯ С ВНИМАНИЯ</div>
          <h1>
            Модерация материалов<span className="heading-dot">.</span>
          </h1>
          <p>Помогаем библиотеке оставаться полезной и понятной.</p>
        </div>
        <span className="pill">
          <ShieldCheck size={16} />
          Демо: роль модератора
        </span>
      </div>
      <div className="info-box">
        Роль доступна демоаккаунту. В рабочей версии права и решения модератора
        должен проверять сервер.
      </div>
      <div className="moderation-tabs">
        {(["pending", "published", "rejected", "draft"] as const).map((t) => (
          <button
            className={tab === t ? "active" : ""}
            key={t}
            onClick={() => setTab(t)}
          >
            {statusText[t]}
            <span>{db.notes.filter((n) => n.status === t).length}</span>
          </button>
        ))}
      </div>
      <div className="moderation-list">
        {db.notes
          .filter((n) => n.status === tab)
          .map((n) => (
            <article className="moderation-card" key={n.id}>
              <div className={`moderation-icon ${subjectColors[n.subject]}`}>
                <FileText size={25} />
              </div>
              <div>
                <span className="muted small-text">
                  {n.subject} · {n.type} · {formatDate(n.createdAt)}
                </span>
                <button
                  className="moderation-title"
                  onClick={() => go(`/notes/${n.id}`)}
                >
                  {n.title}
                </button>
                <p>{n.description}</p>
                <span className="small-text">
                  Автор: {db.users.find((u) => u.id === n.authorId)?.name}
                </span>
                {n.rejection && (
                  <p className="form-error">Причина: {n.rejection}</p>
                )}
              </div>
              <div className="moderation-actions">
                <button
                  className="button secondary small"
                  onClick={() => go(`/notes/${n.id}`)}
                >
                  Посмотреть <ArrowRight size={14} />
                </button>
                {n.status !== "published" && (
                  <button
                    className="button primary small"
                    onClick={() => moderate(n.id, "published")}
                  >
                    <Check size={15} />
                    Одобрить
                  </button>
                )}
                {n.status !== "rejected" && (
                  <button
                    className="text-button danger"
                    onClick={() => {
                      setReason("");
                      setRejectId(n.id);
                    }}
                  >
                    Отклонить
                  </button>
                )}
              </div>
            </article>
          ))}
        {!db.notes.some((n) => n.status === tab) && (
          <div className="empty">
            <CheckCircle2 size={36} />
            <h2>Всё разобрано</h2>
            <p>В этой очереди пока нет материалов.</p>
          </div>
        )}
      </div>
      {rejectId && (
        <Modal title="Причина отклонения" close={() => setRejectId(null)}>
          <p>Объясните автору, что нужно исправить.</p>
          <textarea
            autoFocus
            aria-label="Причина отклонения"
            maxLength={500}
            placeholder="Например, добавьте источники и проверьте формулы"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
          <button
            className="button primary full"
            disabled={!reason.trim()}
            onClick={() => moderate(rejectId, "rejected", reason.trim())}
          >
            Отклонить материал
          </button>
        </Modal>
      )}
    </>
  );
}
function Profile({ logout }: { logout: () => void }) {
  const { db, user, go } = useApp();
  if (!user)
    return (
      <div className="empty">
        <UserRound />
        <h2>Вы ещё не вошли</h2>
        <button className="button primary" onClick={() => go("/login")}>
          Войти
        </button>
      </div>
    );
  const notes = db.notes.filter(
    (n) => n.authorId === user.id && n.status === "published",
  );
  return (
    <>
      <div className="page-heading">
        <div>
          <div className="eyebrow">ВАШЕ МЕСТО В СООБЩЕСТВЕ</div>
          <h1>
            Мой профиль<span className="heading-dot">.</span>
          </h1>
          <p>Ваши знания помогают другим двигаться вперёд.</p>
        </div>
        <button className="button secondary" onClick={logout}>
          <LogOut size={17} />
          Выйти
        </button>
      </div>
      <section className="profile-card">
        <Avatar user={user} size="huge" />
        <div>
          <span className="pill">
            {user.id === demoUser.id
              ? "Демоаккаунт · модератор"
              : "Демонстрационный студент"}
          </span>
          <h2>{user.name}</h2>
          <p>{user.faculty}</p>
          <span className="muted">{user.email}</span>
        </div>
        <div className="profile-stat">
          <strong>{notes.length}</strong>
          <span>
            опубликованных
            <br />
            материалов
          </span>
        </div>
      </section>
      <div className="profile-links">
        <button onClick={() => go("/my")}>
          <FileText />
          <strong>Мои материалы</strong>
          <span>Публикации, черновики и статусы</span>
          <ArrowRight />
        </button>
        <button onClick={() => go("/saved")}>
          <Bookmark />
          <strong>Избранное</strong>
          <span>
            {db.notes.filter((n) => n.saves.includes(user.id)).length}{" "}
            сохранённых материалов
          </span>
          <ArrowRight />
        </button>
      </div>
      <h2 className="profile-heading">Мои публикации</h2>
      <div className="material-grid">
        {notes.map((n) => (
          <NoteCard note={n} key={n.id} />
        ))}
      </div>
    </>
  );
}
function Auth({
  register,
  login,
}: {
  register: boolean;
  login: (id: string) => void;
}) {
  const { db, go, update } = useApp(),
    [name, setName] = useState(""),
    [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    if (register) {
      if (
        db.users.some(
          (u) => u.email?.toLowerCase() === email.trim().toLowerCase(),
        )
      ) {
        setError("Такой демоаккаунт уже есть. Откройте вход.");
        return;
      }
      setBusy(true);
      const u: User = {
        id: crypto.randomUUID(),
        name: name.trim(),
        initials: name
          .trim()
          .split(/\s+/)
          .slice(0, 2)
          .map((s) => s[0])
          .join("")
          .toUpperCase(),
        email: email.trim().toLowerCase(),
        faculty: "Студент сообщества StudyVault",
        color: "purple",
      };
      if (await update((db) => ({ ...db, users: [...db.users, u] })))
        login(u.id);
      setBusy(false);
    } else {
      const u = db.users.find(
        (u) => u.email?.toLowerCase() === email.trim().toLowerCase(),
      );
      if (u) login(u.id);
      else
        setError(
          "Демоаккаунт не найден. Зарегистрируйтесь или выберите «Войти в демо».",
        );
    }
  };
  return (
    <div className="auth-page">
      <section className="auth-story">
        <button className="brand" onClick={() => go("/")}>
          <span className="brand-mark">
            <BookOpen size={23} />
          </span>
          study<span>vault</span>.
        </button>
        <div>
          <span className="hero-label">
            <Sparkles size={16} />
            ОТ СТУДЕНТОВ — СТУДЕНТАМ
          </span>
          <h1>
            Хорошие знания
            <br />
            стоит разделить.
          </h1>
          <p>
            Собирайте конспекты, помогайте друг другу
            <br />и готовьтесь к следующей сессии вместе.
          </p>
          <div className="auth-features">
            <span>
              <CheckCircle2 />
              Материалы по вашим предметам
            </span>
            <span>
              <CheckCircle2 />
              Обсуждения и оценки сообщества
            </span>
            <span>
              <CheckCircle2 />
              Всё важное — в избранном
            </span>
          </div>
        </div>
        <span className="auth-footer">Маленький конспект. Большая помощь.</span>
      </section>
      <section className="auth-form-section">
        <button className="back-link" onClick={() => go("/")}>
          <ArrowLeft size={16} />В библиотеку
        </button>
        <form className="auth-form" onSubmit={submit}>
          <span className="auth-icon">
            <GraduationCap size={30} />
          </span>
          <h1>{register ? "Присоединяйтесь" : "С возвращением"}</h1>
          <p>
            {register
              ? "Ваша первая полезная находка уже ждёт."
              : "Продолжим собирать знания вместе?"}
          </p>
          <div className="info-box">
            Демоверсия: используйте вымышленные данные. Пароль не сохраняется и
            не проверяется сервером.
          </div>
          {register && (
            <label className="field-label">
              Имя и фамилия
              <input
                required
                minLength={2}
                maxLength={60}
                value={name}
                autoComplete="name"
                placeholder="Алексей Смирнов"
                onChange={(e) => setName(e.target.value)}
              />
            </label>
          )}
          <label className="field-label">
            Электронная почта
            <input
              type="email"
              required
              value={email}
              autoComplete="email"
              placeholder="student@example.test"
              onChange={(e) => setEmail(e.target.value)}
            />
          </label>
          <label className="field-label">
            Демонстрационный пароль
            <input
              type="password"
              required
              minLength={8}
              maxLength={128}
              value={password}
              autoComplete={register ? "new-password" : "current-password"}
              placeholder="Не менее 8 символов"
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          {error && (
            <p className="form-error" role="alert">
              {error}
            </p>
          )}
          <button className="button primary full" disabled={busy}>
            {register ? "Создать демоаккаунт" : "Войти"}
            <ArrowRight size={17} />
          </button>
          <div className="auth-divider">
            <span />
            или попробуйте проект
            <span />
          </div>
          <button
            type="button"
            className="button secondary full"
            onClick={() => login(demoUser.id)}
          >
            <Sparkles size={17} />
            Войти в демо
          </button>
          <p className="auth-switch">
            {register ? "Уже есть аккаунт?" : "Впервые здесь?"}{" "}
            <button
              type="button"
              onClick={() => go(register ? "/login" : "/register")}
            >
              {register ? "Войти" : "Зарегистрироваться"}
            </button>
          </p>
        </form>
      </section>
    </div>
  );
}
const rootRoute = createRootRoute({ component: App });
const routeTree = rootRoute.addChildren(
  [
    "/",
    "/saved",
    "/my",
    "/ranking",
    "/moderation",
    "/profile",
    "/new",
    "/login",
    "/register",
    "/notes/$noteId",
    "/edit/$noteId",
  ].map((path) => createRoute({ getParentRoute: () => rootRoute, path })),
);
const router = createRouter({
  routeTree,
  defaultNotFoundComponent: () => null,
});
declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
