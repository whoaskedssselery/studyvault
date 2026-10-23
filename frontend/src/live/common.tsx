import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { api, ApiError } from "../api/client";
import type { ApiUser } from "../api/client";
import type { components } from "../api/schema";
import type { User } from "../data";

export type Models = components["schemas"];
type Outcome<T> = { ok: true; data: T } | { ok: false };
export type LiveState = {
  user: ApiUser | null;
  restoring: boolean;
  busy: boolean;
  go: (path: string) => void;
  setUser: (user: ApiUser | null) => void;
  run: <T>(action: () => Promise<T>, message?: string) => Promise<Outcome<T>>;
  notice: (message: string) => void;
  requireUser: () => ApiUser | null;
};
export const LiveContext = createContext<LiveState>(null!);
export const useLive = () => useContext(LiveContext);
export const statuses: Record<Models["MaterialStatus"], string> = {
  draft: "Черновик",
  pending: "На проверке",
  published: "Опубликован",
  rejected: "Отклонён",
};
export const kindLabel = (kind: Models["MaterialKind"]) =>
  kind === "note" ? "Конспект" : "Шпаргалка";
export const ratingLabel = (value: Models["RatingSummary"]) =>
  value.average === null ? "Нет оценок" : value.average.toFixed(1);
export function viewUser(user: {
  id: string;
  display_name: string;
  faculty: string | null;
  email?: string;
}): User {
  const colors = ["purple", "blue", "orange", "green", "pink"];
  let hash = 0;
  for (const character of user.id)
    hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
  return {
    id: user.id,
    name: user.display_name,
    initials: user.display_name
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .map((word) => word[0] || "")
      .join("")
      .toUpperCase(),
    faculty: user.faculty || "Студент StudyVault",
    email: user.email,
    color: colors[hash % colors.length],
  };
}
export function Avatar({
  author,
  large = false,
}: {
  author: Models["Author"];
  large?: boolean;
}) {
  const user = viewUser(author);
  return (
    <span className={`avatar ${user.color} ${large ? "large" : ""}`}>
      {user.initials}
    </span>
  );
}
export function ErrorBox({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  if (!error) return null;
  return (
    <div className="live-error" role="alert">
      <p>
        {error instanceof Error ? error.message : "Не удалось получить данные."}
      </p>
      {retry && (
        <button className="button secondary" onClick={retry}>
          Повторить
        </button>
      )}
    </div>
  );
}
export function Loading() {
  return (
    <div className="loading" role="status">
      Загружаем…
    </div>
  );
}
export function LoginRequired() {
  const { go } = useLive();
  return (
    <div className="empty">
      <h1>Войдите в аккаунт</h1>
      <p>Ваши материалы и избранное доступны после входа.</p>
      <button className="button primary" onClick={() => go("/login")}>
        Войти
      </button>
    </div>
  );
}
export function Heading({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <div className="eyebrow">ЗНАНИЯ, КОТОРЫМИ ДЕЛЯТСЯ</div>
        <h1>
          {title}
          <span className="heading-dot">.</span>
        </h1>
        {description && <p>{description}</p>}
      </div>
      {children}
    </div>
  );
}
export function NextPage({
  hasNext,
  loading,
  load,
}: {
  hasNext: boolean;
  loading: boolean;
  load: () => void;
}) {
  return hasNext ? (
    <button
      className="button secondary live-more"
      disabled={loading}
      onClick={load}
    >
      {loading ? "Загружаем…" : "Показать ещё"}
    </button>
  ) : null;
}
export function useDebounced(value: string) {
  const [ready, setReady] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setReady(value), 300);
    return () => clearTimeout(timer);
  }, [value]);
  return ready;
}
export function useCooldown() {
  const [until, setUntil] = useState(0),
    [now, setNow] = useState(Date.now());
  const seconds = Math.max(0, Math.ceil((until - now) / 1000));
  useEffect(() => {
    if (!seconds) return;
    const timer = setInterval(() => setNow(Date.now()), 500);
    return () => clearInterval(timer);
  }, [seconds]);
  return {
    seconds,
    start: (seconds: number) => {
      setNow(Date.now());
      setUntil(Date.now() + seconds * 1000);
    },
  };
}
export function VerificationBanner() {
  const { user, busy, run } = useLive();
  const cooldown = useCooldown();
  const [available, setAvailable] = useState(true);
  useEffect(() => {
    setAvailable(true);
    cooldown.start(0);
  }, [user?.id]);
  if (!user || user.email_verified) return null;
  return (
    <section className="live-verification" aria-label="Подтверждение почты">
      <div>
        <strong>Подтвердите почту</strong>
        <p>
          Откройте ссылку из письма, чтобы публиковать материалы, комментировать
          и ставить оценки.
        </p>
      </div>
      {available && (
        <button
          className="button secondary"
          disabled={busy || !!cooldown.seconds}
          onClick={async () => {
            const result = await run(async () => {
              try {
                await api.resendVerification();
                cooldown.start(60);
              } catch (error) {
                if (error instanceof ApiError && error.status === 429)
                  cooldown.start(Number(error.retryAfter) || 60);
                if (
                  error instanceof ApiError &&
                  error.code === "email_unavailable"
                )
                  setAvailable(false);
                throw error;
              }
            }, "Письмо подтверждения отправлено. Проверьте почту.");
            if (result.ok) cooldown.start(60);
          }}
        >
          {cooldown.seconds
            ? `Повторить через ${cooldown.seconds} сек.`
            : "Отправить ещё раз"}
        </button>
      )}
    </section>
  );
}
const plain = (text: string) =>
  text
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, "&");
export function SafeSnippet({ value }: { value: string }) {
  return (
    <>
      {value
        .split(/(<mark>.*?<\/mark>)/gs)
        .map((part, index) =>
          part.startsWith("<mark>") && part.endsWith("</mark>") ? (
            <mark key={index}>{plain(part.slice(6, -7))}</mark>
          ) : (
            <span key={index}>{plain(part)}</span>
          ),
        )}
    </>
  );
}
export function safeFileUrl(value: string) {
  const parsed = new URL(value);
  if (!["http:", "https:"].includes(parsed.protocol))
    throw new Error("Ссылка на файл недействительна.");
  return parsed.href;
}
export function Dialog({
  title,
  close,
  children,
  placement = "center",
}: {
  title: string;
  close: () => void;
  children: ReactNode;
  placement?: "center" | "drawer";
}) {
  const panel = useRef<HTMLDivElement>(null),
    closeRef = useRef(close);
  closeRef.current = close;
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    (
      panel.current?.querySelector<HTMLElement>("[data-autofocus]") ||
      panel.current?.querySelector<HTMLElement>("input,textarea,button")
    )?.focus();
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        closeRef.current();
      }
      if (event.key !== "Tab") return;
      const fields = panel.current?.querySelectorAll<HTMLElement>(
        "button:not(:disabled),input:not(:disabled),textarea:not(:disabled),select:not(:disabled),a[href]",
      );
      if (!fields?.length) return;
      const first = fields[0],
        last = fields[fields.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      }
      if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("keydown", key);
      document.body.style.overflow = overflow;
      previous?.focus();
    };
  }, []);
  return (
    <div
      className={`modal-backdrop ${placement === "drawer" ? "live-drawer-backdrop" : ""}`}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) close();
      }}
    >
      <div
        className={`modal ${placement === "drawer" ? "live-drawer" : ""}`}
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        {children}
      </div>
    </div>
  );
}
