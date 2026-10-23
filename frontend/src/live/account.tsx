import { useEffect, useState } from "react";
import {
  ArrowLeft,
  BookOpen,
  CheckCircle2,
  GraduationCap,
} from "lucide-react";
import { api, ApiError } from "../api/client";
import {
  Dialog,
  Heading,
  LoginRequired,
  Avatar,
  useCooldown,
  useLive,
} from "./common";
import { clearMailToken, mailToken } from "./mail-token";

const validatePassword = (password: string) => {
  if (
    Array.from(password).length < 8 ||
    new TextEncoder().encode(password).length > 72
  )
    throw new Error(
      "Пароль должен содержать не менее 8 символов и занимать не более 72 байт.",
    );
};
export function AuthPage({
  register = false,
  forgot = false,
}: {
  register?: boolean;
  forgot?: boolean;
}) {
  const { go, run, setUser, busy } = useLive();
  const cooldown = useCooldown();
  const [name, setName] = useState(""),
    [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [sent, setSent] = useState(false);
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
        <form
          className="auth-form"
          onSubmit={async (event) => {
            event.preventDefault();
            const result = await run(async () => {
              try {
                if (forgot) {
                  await api.resetPassword(email.trim());
                  cooldown.start(60);
                  return null;
                }
                if (register) validatePassword(password);
                return register
                  ? await api.register(email.trim(), password, name.trim())
                  : await api.login(email.trim(), password);
              } catch (error) {
                if (error instanceof ApiError && error.status === 429)
                  cooldown.start(Number(error.retryAfter) || 60);
                throw error;
              }
            });
            if (result.ok) {
              if (forgot) setSent(true);
              else {
                setUser(result.data);
                setPassword("");
                go("/");
              }
            }
          }}
        >
          <span className="auth-icon">
            <GraduationCap size={30} />
          </span>
          <h1>
            {forgot
              ? "Восстановить пароль"
              : register
                ? "Присоединяйтесь"
                : "С возвращением"}
          </h1>
          <p>
            {forgot
              ? "Введите почту, указанную при регистрации."
              : register
                ? "Создайте аккаунт и делитесь знаниями."
                : "Продолжим собирать знания вместе?"}
          </p>
          {sent && (
            <p role="status" className="info-box">
              Если адрес зарегистрирован, на него отправлена ссылка для сброса
              пароля. Проверьте почту.
            </p>
          )}
          {register && (
            <label className="field-label">
              Имя и фамилия
              <input
                required
                maxLength={100}
                autoComplete="name"
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
          )}
          <label className="field-label">
            Электронная почта
            <input
              type="email"
              required
              maxLength={254}
              autoComplete="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          {!forgot && (
            <label className="field-label">
              Пароль
              <input
                type="password"
                required
                minLength={register ? 8 : 1}
                maxLength={72}
                autoComplete={register ? "new-password" : "current-password"}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            </label>
          )}
          <button
            className="button primary full"
            disabled={busy || !!cooldown.seconds}
          >
            {cooldown.seconds
              ? `Повторить через ${cooldown.seconds} сек.`
              : forgot
                ? "Отправить ссылку"
                : register
                  ? "Создать аккаунт"
                  : "Войти"}
          </button>
          {!register && !forgot && (
            <button
              type="button"
              className="text-button live-forgot"
              onClick={() => go("/forgot-password")}
            >
              Забыли пароль?
            </button>
          )}
          <p className="auth-switch">
            {forgot
              ? "Вспомнили пароль?"
              : register
                ? "Уже есть аккаунт?"
                : "Впервые здесь?"}{" "}
            <button
              type="button"
              onClick={() => go(register || forgot ? "/login" : "/register")}
            >
              {register || forgot ? "Войти" : "Зарегистрироваться"}
            </button>
          </p>
        </form>
      </section>
    </div>
  );
}

export function ProfilePage() {
  const { user, setUser, run, busy, go } = useLive();
  const cooldown = useCooldown();
  const [name, setName] = useState(user?.display_name || ""),
    [faculty, setFaculty] = useState(user?.faculty || ""),
    [currentPassword, setCurrentPassword] = useState(""),
    [newPassword, setNewPassword] = useState(""),
    [deleting, setDeleting] = useState(false),
    [deletePassword, setDeletePassword] = useState("");
  useEffect(() => {
    setName(user?.display_name || "");
    setFaculty(user?.faculty || "");
  }, [user?.id]);
  if (!user) return <LoginRequired />;
  const limited = async <T,>(action: () => Promise<T>) => {
    try {
      return await action();
    } catch (error) {
      if (error instanceof ApiError && error.status === 429)
        cooldown.start(Number(error.retryAfter) || 60);
      throw error;
    }
  };
  return (
    <>
      <Heading
        title="Мой профиль"
        description="Информация об аккаунте и настройки безопасности."
      />
      <section className="profile-card">
        <Avatar author={user} large />
        <div>
          <span className="pill">
            {user.role === "moderator" ? "Модератор" : "Студент"}
          </span>
          <h2>{user.display_name}</h2>
          <p>{user.faculty || "Факультет не указан"}</p>
          <span className="muted">
            {user.email} ·{" "}
            {user.email_verified
              ? "Почта подтверждена"
              : "Почта не подтверждена"}
          </span>
        </div>
      </section>
      <div className="live-profile-grid">
        <section className="editor-card">
          <h2>О себе</h2>
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              const result = await run(
                () =>
                  api.profile({
                    display_name: name.trim(),
                    faculty: faculty.trim() || null,
                  }),
                "Профиль обновлён",
              );
              if (result.ok) setUser(result.data);
            }}
          >
            <label className="field-label">
              Имя и фамилия
              <input
                required
                maxLength={100}
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            <label className="field-label">
              Факультет
              <input
                maxLength={100}
                value={faculty}
                onChange={(event) => setFaculty(event.target.value)}
                placeholder="Необязательно"
              />
            </label>
            <button className="button primary" disabled={busy || !name.trim()}>
              Сохранить профиль
            </button>
          </form>
        </section>
        <section className="editor-card">
          <h2>Сменить пароль</h2>
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              const result = await run(
                () =>
                  limited(async () => {
                    validatePassword(newPassword);
                    if (newPassword === currentPassword)
                      throw new Error(
                        "Новый пароль должен отличаться от текущего.",
                      );
                    await api.changePassword(currentPassword, newPassword);
                  }),
                "Пароль изменён. Остальные сессии завершены.",
              );
              if (result.ok) {
                setCurrentPassword("");
                setNewPassword("");
              }
            }}
          >
            <label className="field-label">
              Текущий пароль
              <input
                type="password"
                required
                maxLength={72}
                autoComplete="current-password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
              />
            </label>
            <label className="field-label">
              Новый пароль
              <input
                type="password"
                required
                minLength={8}
                maxLength={72}
                autoComplete="new-password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
              />
            </label>
            <button
              className="button primary"
              disabled={busy || !!cooldown.seconds}
            >
              {cooldown.seconds
                ? `Повторить через ${cooldown.seconds} сек.`
                : "Изменить пароль"}
            </button>
          </form>
        </section>
      </div>
      <section className="aside-panel live-danger">
        <h2>Удаление аккаунта</h2>
        <p>
          Ваши материалы, комментарии, оценки, файлы и сессии будут удалены. Это
          действие нельзя отменить.
        </p>
        <button className="button secondary" onClick={() => setDeleting(true)}>
          Удалить аккаунт
        </button>
      </section>
      {deleting && (
        <Dialog
          title="Удалить аккаунт"
          close={() => {
            if (!busy) {
              setDeleting(false);
              setDeletePassword("");
            }
          }}
        >
          <h2>Удалить аккаунт?</h2>
          <p>Введите текущий пароль для подтверждения.</p>
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              const result = await run(
                () => limited(() => api.deleteAccount(deletePassword)),
                "Аккаунт удалён",
              );
              if (result.ok) {
                setUser(null);
                setDeletePassword("");
                go("/");
              }
            }}
          >
            <label className="field-label">
              Пароль для удаления
              <input
                type="password"
                autoComplete="current-password"
                required
                value={deletePassword}
                onChange={(event) => setDeletePassword(event.target.value)}
              />
            </label>
            <div className="live-actions">
              <button
                className="button primary"
                disabled={busy || !!cooldown.seconds}
              >
                {cooldown.seconds
                  ? `Повторить через ${cooldown.seconds} сек.`
                  : "Удалить навсегда"}
              </button>
              <button
                type="button"
                className="button secondary"
                onClick={() => {
                  setDeleting(false);
                  setDeletePassword("");
                }}
              >
                Отмена
              </button>
            </div>
          </form>
        </Dialog>
      )}
    </>
  );
}

let verificationRequest: Promise<void> | null = null;
function verifyOnce(token: string) {
  if (!verificationRequest)
    verificationRequest = api.verifyEmail(token).catch((error) => {
      if (!(error instanceof ApiError) || error.code !== "invalid_token")
        verificationRequest = null;
      throw error;
    });
  return verificationRequest;
}
export function VerifyEmailPage() {
  const { user, setUser, go } = useLive();
  const [token] = useState(() => mailToken("verification")),
    [state, setState] = useState<"loading" | "success" | "error">("loading"),
    [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    if (!token) {
      setState("error");
      setError("В ссылке нет токена подтверждения. Запросите новое письмо.");
      return;
    }
    verifyOnce(token)
      .then(async () => {
        clearMailToken("verification");
        if (user) {
          try {
            const current = await api.me();
            if (active) setUser(current);
          } catch {}
        }
        if (active) setState("success");
      })
      .catch((error) => {
        if (error instanceof ApiError && error.code === "invalid_token")
          clearMailToken("verification");
        if (active) {
          setState("error");
          setError(error.message);
        }
      });
    return () => {
      active = false;
    };
  }, [token, user?.id]);
  return (
    <section className="editor-card live-mail-card">
      <h1>
        {state === "loading"
          ? "Подтверждаем почту…"
          : state === "success"
            ? "Почта подтверждена"
            : "Не удалось подтвердить почту"}
      </h1>
      {state === "error" && <p role="alert">{error}</p>}
      {state === "success" && (
        <p>
          Теперь можно публиковать материалы, комментировать и ставить оценки.
        </p>
      )}
      {state !== "loading" && (
        <button
          className="button primary"
          onClick={() => go(user ? "/" : "/login")}
        >
          {user ? "В библиотеку" : "Войти"}
        </button>
      )}
      {state === "error" && user && (
        <p className="muted">
          Запросите новое письмо с помощью панели «Подтвердите почту».
        </p>
      )}
    </section>
  );
}

export function ResetPasswordPage() {
  const { run, busy, setUser, go } = useLive();
  const cooldown = useCooldown();
  const [token] = useState(() => mailToken("reset")),
    [password, setPassword] = useState(""),
    [confirmation, setConfirmation] = useState("");
  return (
    <section className="editor-card live-mail-card">
      <h1>Новый пароль</h1>
      {!token ? (
        <>
          <p role="alert">
            Ссылка недействительна или уже использована. Запросите новую.
          </p>
          <button
            className="button primary"
            onClick={() => go("/forgot-password")}
          >
            Запросить новую ссылку
          </button>
        </>
      ) : (
        <form
          onSubmit={async (event) => {
            event.preventDefault();
            const result = await run(async () => {
              validatePassword(password);
              if (password !== confirmation)
                throw new Error("Пароли не совпадают.");
              try {
                await api.confirmPasswordReset(token, password);
              } catch (error) {
                if (error instanceof ApiError && error.status === 429)
                  cooldown.start(Number(error.retryAfter) || 60);
                throw error;
              }
            }, "Пароль обновлён. Войдите с новым паролем.");
            if (result.ok) {
              clearMailToken("reset");
              setUser(null);
              setPassword("");
              go("/login");
            }
          }}
        >
          <label className="field-label">
            Новый пароль
            <input
              type="password"
              autoComplete="new-password"
              required
              minLength={8}
              maxLength={72}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          <label className="field-label">
            Повторите пароль
            <input
              type="password"
              autoComplete="new-password"
              required
              maxLength={72}
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </label>
          <button
            className="button primary"
            disabled={busy || !!cooldown.seconds}
          >
            {cooldown.seconds
              ? `Повторить через ${cooldown.seconds} сек.`
              : "Сохранить новый пароль"}
          </button>
          <button
            type="button"
            className="text-button live-forgot"
            onClick={() => go("/forgot-password")}
          >
            Запросить другую ссылку
          </button>
        </form>
      )}
    </section>
  );
}
