// Optional local server. The submission itself only needs index.html + mock.json.
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const root = dirname(fileURLToPath(import.meta.url));
const option = process.argv.indexOf("--port");
const port = Number(option === -1 ? 4173 : process.argv[option + 1]);
if (!Number.isInteger(port) || port < 1 || port > 65535)
  throw new Error("Port must be between 1 and 65535");
const target = new URL(
  process.env.STUDYVAULT_API_TARGET || "http://127.0.0.1:8080",
);
if (!["http:", "https:"].includes(target.protocol))
  throw new Error("API target must be an HTTP(S) URL");
function json(res, status, value) {
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify(value));
}
const server = createServer(async (req, res) => {
  const requested = new URL(req.url, "http://localhost");
  const pathname = requested.pathname;
  if (pathname.startsWith("/backend-api/")) {
    const endpoint = pathname.slice("/backend-api".length);
    if (
      !/^\/(health|auth\/(login|register|refresh|logout|password-reset(?:\/confirm)?|email-verification\/confirm)|me(?:\/(password|email-verification))?|notes(?:\/[0-9a-f-]+(?:\/(tags|backlinks|submit|withdraw))?)?|subjects(?:\/[0-9a-f-]+)?|tags(?:\/[0-9a-f-]+)?|attachments(?:\/[0-9a-f-]+)?|search|graph|library-subjects(?:\/[0-9a-f-]+)?|library(?:\/[0-9a-f-]+(?:\/(comments|rating|bookmark|attachments\/[0-9a-f-]+))?)?|comments\/[0-9a-f-]+|bookmarks|moderation\/(queue|[0-9a-f-]+\/(decision|unpublish))|ranking\/authors|authors\/[0-9a-f-]+)$/.test(
        endpoint,
      )
    )
      return json(res, 404, {
        error: { code: "not_found", message: "Unknown API endpoint" },
      });
    if (!["GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"].includes(req.method))
      return json(res, 405, {
        error: { code: "method_not_allowed", message: "Unsupported method" },
      });
    const chunks = [];
    let length = 0;
    for await (const chunk of req) {
      length += chunk.length;
      if (length > 1024 * 1024)
        return json(res, 413, {
          error: { code: "body_too_large", message: "JSON body exceeds 1 MB" },
        });
      chunks.push(chunk);
    }
    const headers = { Accept: "application/json" };
    for (const header of ["authorization", "cookie", "content-type"])
      if (req.headers[header]) headers[header] = req.headers[header];
    try {
      const response = await fetch(
        new URL("/v1" + endpoint + requested.search, target),
        {
          method: req.method,
          headers,
          body: chunks.length ? Buffer.concat(chunks) : undefined,
          signal: AbortSignal.timeout(8000),
          redirect: "error",
        },
      );
      const body = await response.text();
      const output = {
        "Content-Type": response.headers.get("content-type") || "text/plain",
        "Cache-Control": "no-store",
      };
      const cookies = response.headers.getSetCookie();
      if (cookies.length)
        output["Set-Cookie"] = cookies.map((c) =>
          c.replace(/;\s*Path=\/v1\/auth(?=;|$)/gi, "; Path=/backend-api/auth"),
        );
      if (response.headers.has("retry-after"))
        output["Retry-After"] = response.headers.get("retry-after");
      if (response.headers.has("x-request-id"))
        output["X-Request-ID"] = response.headers.get("x-request-id");
      res.writeHead(response.status, output);
      res.end(req.method === "HEAD" ? "" : body);
    } catch {
      json(res, 502, {
        error: {
          code: "api_unavailable",
          message: "Start the API server or set STUDYVAULT_API_TARGET.",
        },
      });
    }
    return;
  }
  if (!["GET", "HEAD"].includes(req.method))
    return json(res, 405, {
      error: {
        code: "method_not_allowed",
        message: "Static files are read-only",
      },
    });
  const file = {
    "/": "index.html",
    "/index.html": "index.html",
    "/mock.json": "mock.json",
  }[pathname];
  if (!file)
    return json(res, 404, {
      error: { code: "not_found", message: "Use hash routes: /#/notes/note-1" },
    });
  try {
    const data = await readFile(resolve(root, file));
    res.writeHead(200, {
      "Content-Type": file.endsWith(".json")
        ? "application/json; charset=utf-8"
        : "text/html; charset=utf-8",
      "Cache-Control": "no-store",
    });
    res.end(req.method === "HEAD" ? undefined : data);
  } catch {
    json(res, 404, {
      error: {
        code: "missing_file",
        message: "Run npm run package:submission from frontend first.",
      },
    });
  }
});
server.listen(port, "127.0.0.1", () =>
  console.log(`StudyVault submission: http://127.0.0.1:${port}/`),
);
server.on("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
