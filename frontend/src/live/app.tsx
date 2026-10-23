import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouter, useRouterState } from "@tanstack/react-router";
import {
  Bell,
  BookOpen,
  Bookmark,
  ChevronRight,
  FileText,
  GraduationCap,
  HelpCircle,
  Menu,
  Moon,
  Plus,
  Search,
  ShieldCheck,
  Sparkles,
  Sun,
  Trophy,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { api, ApiError } from "../api/client";
import type { ApiUser } from "../api/client";
import { AccountMenu } from "../account-menu";
import { useTheme } from "../theme";
import {
  LiveContext,
  Loading,
  VerificationBanner,
  useLive,
  viewUser,
} from "./common";
import { ActivityPanel, HelpPanel, SearchPanel, SubjectsPanel } from "./panels";
import { AuthorPage, LibraryFeed, MyNotes } from "./library";
import { MaterialPage } from "./material";
import {
  AuthPage,
  ProfilePage,
  ResetPasswordPage,
  VerifyEmailPage,
} from "./account";
import { ModerationPage } from "./moderation";
const MaterialEditor = lazy(() =>
  import("./editor").then((module) => ({ default: module.MaterialEditor })),
);
const ApiStatus = lazy(() =>
  import("../api-status").then((module) => ({ default: module.ApiStatus })),
);
const ApiWorkspace = lazy(() =>
  import("../api/workspace").then((module) => ({
    default: module.ApiWorkspace,
  })),
);

export function LiveApp() {
  const cache = useQueryClient(),
    router = useRouter();
  const path = useRouterState({ select: (state) => state.location.pathname });
  const { theme, toggleTheme } = useTheme();
  const [user, setUser] = useState<ApiUser | null>(null),
    [restoring, setRestoring] = useState(true),
    [busy, setBusy] = useState(false),
    [mobile, setMobile] = useState(false),
    [subject, setSubject] = useState(""),
    [message, setMessage] = useState(""),
    [error, setError] = useState(false),
    [panel, setPanel] = useState<
      "search" | "activity" | "subjects" | "help" | null
    >(null);
  const working = useRef(false);
  const go = (route: string) => {
    setPanel(null);
    void router.navigate({ to: route });
    setMobile(false);
    window.scrollTo(0, 0);
  };
  const notice = (text: string) => {
    setMessage(text);
    setError(false);
  };
  useEffect(() => {
    let active = true;
    api
      .restore()
      .then((current) => {
        if (active) setUser(current);
      })
      .catch((error) => {
        if (active && (!(error instanceof ApiError) || error.status !== 401)) {
          setMessage(error.message);
          setError(true);
        }
      })
      .finally(() => {
        if (active) setRestoring(false);
      });
    const ended = () => {
      setUser(null);
      cache.removeQueries({ queryKey: ["live"] });
      cache.removeQueries({ queryKey: ["api"] });
    };
    window.addEventListener("studyvault-api-session-ended", ended);
    return () => {
      active = false;
      window.removeEventListener("studyvault-api-session-ended", ended);
    };
  }, [cache]);
  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(() => setMessage(""), error ? 12000 : 5000);
    return () => clearTimeout(timer);
  }, [message, error]);
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPanel((value) => (value === "search" ? null : "search"));
      }
      if (event.key === "Escape") setMobile(false);
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, [router]);
  const run: ReturnType<typeof useLive>["run"] = async (action, success) => {
    if (working.current) return { ok: false };
    working.current = true;
    setBusy(true);
    setMessage("");
    try {
      const data = await action();
      if (success) notice(success);
      return { ok: true, data };
    } catch (error) {
      setError(true);
      setMessage(
        error instanceof ApiError && error.retryAfter
          ? `${error.message} Повторите через ${error.retryAfter} сек.`
          : error instanceof Error
            ? error.message
            : "Не удалось выполнить действие.",
      );
      return { ok: false };
    } finally {
      working.current = false;
      setBusy(false);
      void cache.invalidateQueries({ queryKey: ["live"] });
    }
  };
  const requireUser = () => {
    if (!user) {
      notice("Войдите, чтобы участвовать в сообществе");
      go("/login");
    }
    return user;
  };
  const subjects = useQuery({
    queryKey: ["live", "subjects", user?.id],
    queryFn: api.librarySubjects,
    retry: false,
  });
  const navigation: [string, string, LucideIcon][] = [
    ["/", "Библиотека", BookOpen],
    ["/saved", "Избранное", Bookmark],
    ["/my", "Мои материалы", FileText],
    ["/ranking", "Рейтинг авторов", Trophy],
  ];
  if (user?.role === "moderator")
    navigation.push(["/moderation", "Модерация", ShieldCheck]);
  const title =
    (
      {
        "/login": "Вход",
        "/register": "Регистрация",
        "/forgot-password": "Восстановление пароля",
        "/verify-email": "Подтверждение почты",
        "/reset-password": "Новый пароль",
      } as Record<string, string>
    )[path] ||
    navigation.find(([route]) => route === path)?.[1] ||
    (path.startsWith("/notes/")
      ? "Материал"
      : path.startsWith("/edit/") || path === "/new"
        ? "Редактор"
        : path.startsWith("/authors/")
          ? "Профиль автора"
          : path === "/verify-email"
            ? "Подтверждение почты"
            : path === "/reset-password"
              ? "Новый пароль"
              : "Мой профиль");
  useEffect(() => {
    document.title = `${title} · StudyVault`;
    setPanel(null);
  }, [path, title]);
  const auth = ["/login", "/register", "/forgot-password"].includes(path);
  let page;
  if (path === "/")
    page = <LibraryFeed key={subject} initialSubject={subject} />;
  else if (path === "/saved") page = <LibraryFeed key="saved" saved />;
  else if (path === "/my") page = <MyNotes />;
  else if (path.startsWith("/notes/"))
    page = <MaterialPage key={path} id={decodeURIComponent(path.slice(7))} />;
  else if (path === "/new" || path.startsWith("/edit/"))
    page = (
      <MaterialEditor
        key={path}
        id={
          path.startsWith("/edit/")
            ? decodeURIComponent(path.slice(6))
            : undefined
        }
      />
    );
  else if (path === "/ranking") page = <AuthorPage />;
  else if (path.startsWith("/authors/"))
    page = <AuthorPage key={path} id={decodeURIComponent(path.slice(9))} />;
  else if (path === "/profile") page = <ProfilePage />;
  else if (path === "/moderation") page = <ModerationPage />;
  else if (path === "/verify-email") page = <VerifyEmailPage />;
  else if (path === "/reset-password") page = <ResetPasswordPage />;
  else if (path === "/api-status") page = <ApiStatus />;
  else if (path === "/workspace") page = <ApiWorkspace />;
  else
    page = (
      <div className="empty">
        <h1>Страница не найдена</h1>
        <button className="button primary" onClick={() => go("/")}>
          В библиотеку
        </button>
      </div>
    );
  if (
    restoring &&
    (["/saved", "/my", "/profile", "/new", "/moderation"].includes(path) ||
      path.startsWith("/edit/"))
  )
    page = <Loading />;
  return (
    <LiveContext.Provider
      value={{ user, restoring, busy, go, run, notice, setUser, requireUser }}
    >
      {auth ? (
        <AuthPage
          key={path}
          register={path === "/register"}
          forgot={path === "/forgot-password"}
        />
      ) : (
        <div className="app-shell">
          <a
            className="skip-link"
            href="#main-content"
            onClick={(event) => {
              event.preventDefault();
              document.getElementById("main-content")?.focus();
            }}
          >
            Перейти к содержимому
          </a>
          {mobile && (
            <button
              className="sidebar-overlay"
              aria-label="Закрыть меню"
              onClick={() => setMobile(false)}
            />
          )}
          <aside className={`sidebar ${mobile ? "open" : ""}`}>
            <button
              className="brand"
              onClick={() => {
                setSubject("");
                go("/");
              }}
            >
              <span className="brand-mark">
                <BookOpen size={23} />
              </span>
              study<span>vault</span>
              <span className="brand-dot">.</span>
            </button>
            <span className="brand-caption">ЗНАНИЯ, КОТОРЫМИ ДЕЛЯТСЯ</span>
            <button
              className="sidebar-search"
              onClick={() => {
                setMobile(false);
                setPanel("search");
              }}
            >
              <Search size={16} />
              Быстрый поиск <kbd>Ctrl K</kbd>
            </button>
            <nav aria-label="Основная навигация">
              {navigation.map(([route, label, Icon]) => (
                <button
                  className={`nav-item ${route === path ? "selected" : ""}`}
                  key={route}
                  onClick={() => {
                    if (route === "/") setSubject("");
                    go(route);
                  }}
                >
                  <Icon size={19} />
                  {label}
                </button>
              ))}
            </nav>
            <div className="sidebar-section">
              <span>ПРЕДМЕТЫ</span>
              <button
                aria-label="Все предметы"
                onClick={() => {
                  setMobile(false);
                  setPanel("subjects");
                }}
              >
                <Plus size={15} />
              </button>
            </div>
            <nav aria-label="Предметы">
              {subjects.data?.items.map((item) => (
                <button
                  className={`subject-nav ${item.id === subject ? "selected" : ""}`}
                  key={item.id}
                  onClick={() => {
                    setSubject(item.id);
                    go("/");
                  }}
                >
                  <span
                    className="subject-dot"
                    style={{
                      color: /^#[0-9a-f]{6}$/i.test(item.color || "")
                        ? item.color!
                        : "var(--purple)",
                    }}
                  />
                  {item.title}
                  <span className="subject-count">{item.materials_count}</span>
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
                  Поделиться материалом →
                </button>
              </div>
              <button
                className="help-button"
                onClick={() => {
                  setMobile(false);
                  setPanel("help");
                }}
              >
                <HelpCircle size={17} />О проекте
              </button>
            </div>
          </aside>
          <div className="workspace">
            <header className="topbar">
              <div className="breadcrumb">
                <button
                  className="mobile-menu icon-button"
                  aria-label="Открыть меню"
                  onClick={() => setMobile((value) => !value)}
                >
                  <Menu size={20} />
                </button>
                <GraduationCap size={19} />
                <span>База знаний</span>
                <ChevronRight size={14} />
                <strong>{title}</strong>
              </div>
              <div className="topbar-actions">
                <button
                  className="icon-button"
                  aria-label="Уведомления"
                  onClick={() => setPanel("activity")}
                >
                  <Bell size={18} />
                </button>
                <span className="topbar-divider" />
                {restoring ? (
                  <span className="muted">Проверяем вход…</span>
                ) : user ? (
                  <AccountMenu
                    user={viewUser(user)}
                    navigate={go}
                    logout={() =>
                      void run(async () => {
                        await api.logout();
                        setUser(null);
                        go("/login");
                      })
                    }
                    theme={theme}
                    toggleTheme={toggleTheme}
                  />
                ) : (
                  <>
                    <button
                      className="icon-button"
                      aria-label={
                        theme === "dark" ? "Светлая тема" : "Тёмная тема"
                      }
                      onClick={toggleTheme}
                    >
                      {theme === "dark" ? (
                        <Sun size={18} />
                      ) : (
                        <Moon size={18} />
                      )}
                    </button>
                    <button
                      className="button primary small"
                      onClick={() => go("/login")}
                    >
                      Войти
                    </button>
                  </>
                )}
              </div>
            </header>
            <main className="main-content" id="main-content" tabIndex={-1}>
              <VerificationBanner />
              <Suspense fallback={<Loading />}>{page}</Suspense>
            </main>
            <footer className="page-footer">
              <span>StudyVault © 2026</span>
              <span>От студентов — студентам ♡</span>
            </footer>
          </div>
        </div>
      )}
      {panel === "search" && <SearchPanel close={() => setPanel(null)} />}
      {panel === "activity" && <ActivityPanel close={() => setPanel(null)} />}
      {panel === "help" && <HelpPanel close={() => setPanel(null)} />}
      {panel === "subjects" && (
        <SubjectsPanel
          subjects={subjects.data?.items || []}
          error={subjects.error}
          pending={subjects.isPending}
          retry={() => void subjects.refetch()}
          close={() => setPanel(null)}
          select={(id) => {
            setSubject(id);
            go("/");
          }}
        />
      )}
      {message && (
        <div
          className={`toast live-notice ${error ? "live-error" : ""}`}
          role={error ? "alert" : "status"}
        >
          <span>{message}</span>
          <button
            className="icon-button"
            aria-label="Закрыть сообщение"
            onClick={() => setMessage("")}
          >
            ×
          </button>
        </div>
      )}
    </LiveContext.Provider>
  );
}
