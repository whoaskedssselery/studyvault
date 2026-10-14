import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, FileText, Trash2, Upload } from "lucide-react";
import { api } from "./client";
import type { components } from "./schema";
type Mime = components["schemas"]["AttachmentInput"]["mime_type"];
const types = [
  "application/pdf",
  "image/png",
  "image/jpeg",
  "image/webp",
] as const satisfies readonly Mime[];
function url(value: string) {
  const parsed = new URL(value);
  if (!["https:", "http:"].includes(parsed.protocol))
    throw new Error("Сервер вернул недопустимую ссылку на файл.");
  return parsed.href;
}
export function ApiFiles({
  userId,
  noteId,
  userFacing = false,
}: {
  userId: string;
  noteId: string;
  userFacing?: boolean;
}) {
  const cache = useQueryClient(),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [retry, setRetry] = useState<{ file: File; uploadUrl: string } | null>(
      null,
    );
  const files = useQuery({
    queryKey: ["api", userId, "attachments", noteId],
    queryFn: () => api.attachments(noteId),
    retry: false,
  });
  const run = async (operation: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await operation();
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : "Не удалось выполнить действие с файлом.",
      );
    } finally {
      setBusy(false);
      void cache.invalidateQueries({
        queryKey: ["api", userId, "attachments", noteId],
      });
      void cache.invalidateQueries({ queryKey: ["live"] });
    }
  };
  const put = async (file: File, uploadUrl: string) => {
    let response: Response;
    try {
      response = await fetch(url(uploadUrl), {
        method: "PUT",
        body: file,
        headers: { "Content-Type": file.type || guess(file.name) },
        credentials: "omit",
        redirect: "error",
        signal: AbortSignal.timeout(30000),
      });
    } catch {
      throw new Error(
        userFacing
          ? "Не удалось загрузить файл. Проверьте соединение и повторите загрузку. Если ошибка остаётся, обратитесь к администратору."
          : "Файл зарегистрирован, но загрузка в S3 не завершилась. Проверьте адрес хранилища и CORS, затем повторите загрузку.",
      );
    }
    if (!response.ok)
      throw new Error(
        userFacing
          ? "Не удалось загрузить файл. Попробуйте ещё раз. Если ссылка истекла, удалите запись файла и выберите его заново."
          : `S3 отклонил загрузку: HTTP ${response.status}. При истёкшей ссылке удалите незавершённую запись и выберите файл заново.`,
      );
    setRetry(null);
  };
  return (
    <section className="aside-panel api-files">
      <h3>{userFacing ? "Вложения" : "Файлы на сервере"}</h3>
      <p className="muted">
        PDF, PNG, JPEG или WebP · до 3 файлов по 2 МБ.
        {!userFacing && " Загрузка идёт напрямую в S3 по подписанной ссылке."}
      </p>
      <label className="upload-zone">
        <Upload size={22} />
        <strong>
          {busy ? "Выполняем действие…" : "Добавить файл к заметке"}
        </strong>
        <input
          type="file"
          disabled={
            busy || files.isPending || (files.data?.items.length || 0) >= 3
          }
          accept=".pdf,.png,.jpg,.jpeg,.webp"
          aria-label={userFacing ? "Прикрепить файл" : "Файл API"}
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (!file) return;
            const mime = file.type || guess(file.name);
            if (
              !types.some((type) => type === mime) ||
              file.size < 1 ||
              file.size > 2 * 1024 * 1024
            ) {
              setError("Выберите PDF, PNG, JPEG или WebP от 1 байта до 2 МБ.");
              return;
            }
            if ((files.data?.items.length || 0) >= 3) {
              setError("К заметке можно прикрепить не больше 3 файлов.");
              return;
            }
            void run(async () => {
              const registered = await api.registerAttachment({
                note_id: noteId,
                file_name: file.name,
                mime_type: mime as Mime,
                size_bytes: file.size,
              });
              setRetry({ file, uploadUrl: registered.upload_url });
              await put(file, registered.upload_url);
            });
          }}
        />
      </label>
      {retry && (
        <button
          className="button secondary small"
          disabled={busy}
          onClick={() => void run(() => put(retry.file, retry.uploadUrl))}
        >
          Повторить загрузку: {retry.file.name}
        </button>
      )}
      {(error || files.error) && (
        <p className="form-error" role="alert">
          {error || files.error?.message}
        </p>
      )}
      {files.data?.items.map((file) => (
        <div className="attachment" key={file.id}>
          <FileText size={20} />
          <span>
            {file.file_name}
            <small>{Math.ceil(file.size_bytes / 1024)} КБ</small>
          </span>
          <button
            className="icon-button"
            aria-label={`Скачать файл ${file.file_name}`}
            disabled={busy}
            onClick={() =>
              void run(async () => {
                const download = await api.attachment(file.id);
                const a = document.createElement("a");
                a.href = url(download.download_url);
                a.target = "_blank";
                a.rel = "noopener noreferrer";
                a.click();
              })
            }
          >
            <Download size={18} />
          </button>
          <button
            className="icon-button"
            aria-label={`Удалить файл ${file.file_name}`}
            disabled={busy}
            onClick={() => {
              if (window.confirm(`Удалить файл «${file.file_name}»${userFacing ? "" : " из S3"}?`))
                void run(async () => {
                  await api.deleteAttachment(file.id);
                  setRetry(null);
                });
            }}
          >
            <Trash2 size={17} />
          </button>
        </div>
      ))}
      {files.data?.items.length === 0 && (
        <p className="muted">Вложений пока нет.</p>
      )}
    </section>
  );
}
function guess(name: string): string {
  const ext = name.split(".").pop()?.toLowerCase();
  return (
    (
      {
        pdf: "application/pdf",
        png: "image/png",
        jpg: "image/jpeg",
        jpeg: "image/jpeg",
        webp: "image/webp",
      } as Record<string, string>
    )[ext || ""] || ""
  );
}
