import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Search, Share2 } from "lucide-react";
import { api } from "./client";
export function KnowledgeTools({
  userId,
  onOpen,
}: {
  userId: string;
  onOpen: (id: string) => void;
}) {
  const [tab, setTab] = useState("search"),
    [draft, setDraft] = useState(""),
    [query, setQuery] = useState("");
  const search = useQuery({
    queryKey: ["api", userId, "search", query],
    queryFn: () => api.search(query),
    enabled: !!query,
    retry: false,
  });
  const graph = useQuery({
    queryKey: ["api", userId, "graph"],
    queryFn: api.graph,
    enabled: tab === "graph",
    retry: false,
  });
  const shown = graph.data?.nodes.slice(0, 20) || [];
  const positions = new Map(
    shown.map((n, i) => [
      n.id,
      {
        x: 300 + 200 * Math.cos((i * Math.PI * 2) / shown.length),
        y: 160 + 110 * Math.sin((i * Math.PI * 2) / shown.length),
      },
    ]),
  );
  return (
    <section className="api-knowledge aside-panel">
      <div className="material-tabs">
        <button
          className={tab === "search" ? "active" : ""}
          onClick={() => setTab("search")}
        >
          <Search size={14} />
          Поиск API
        </button>
        <button
          className={tab === "graph" ? "active" : ""}
          onClick={() => setTab("graph")}
        >
          <Share2 size={14} />
          Граф связей
        </button>
      </div>
      {tab === "search" ? (
        <>
          <form
            className="api-search"
            onSubmit={(e) => {
              e.preventDefault();
              setQuery(draft.trim());
            }}
          >
            <div className="search-field">
              <Search size={17} />
              <input
                required
                maxLength={200}
                aria-label="Поиск в API"
                placeholder="Поиск по вашим заметкам…"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
              />
            </div>
            <button
              className="button primary"
              disabled={!draft.trim() || search.isFetching}
            >
              Найти в API
            </button>
          </form>
          {search.isFetching && <p className="muted">Ищем на сервере…</p>}
          {search.error && (
            <p className="form-error" role="alert">
              {search.error.message}
            </p>
          )}
          {search.data?.items.map((n) => (
            <button
              className="api-search-result"
              key={n.id}
              onClick={() => onOpen(n.id)}
            >
              <strong>{n.title}</strong>
              <span>
                {n.snippet
                  .split(/(<mark>.*?<\/mark>)/gs)
                  .map((part, i) =>
                    part.startsWith("<mark>") ? (
                      <mark key={i}>{plain(part.slice(6, -7))}</mark>
                    ) : (
                      plain(part)
                    ),
                  )}
              </span>
            </button>
          ))}
          {query && search.data?.items.length === 0 && (
            <p className="muted">По запросу ничего не найдено.</p>
          )}
        </>
      ) : (
        <>
          {graph.isFetching && <p className="muted">Загружаем граф…</p>}
          {graph.error && (
            <p className="form-error" role="alert">
              {graph.error.message}
            </p>
          )}
          {graph.data && (
            <>
              {!shown.length ? (
                <p className="muted">
                  Граф пока пуст. Создайте заметки и свяжите их через
                  [[Название]].
                </p>
              ) : (
                <>
                  <svg
                    className="api-graph"
                    viewBox="0 0 600 320"
                    role="img"
                    aria-label="Граф заметок API"
                  >
                    {graph.data.edges.map((edge, i) => {
                      const a = positions.get(edge.source),
                        b = positions.get(edge.target);
                      return a && b ? (
                        <line
                          key={i}
                          x1={a.x}
                          y1={a.y}
                          x2={b.x}
                          y2={b.y}
                          stroke="#cbb6e2"
                          strokeWidth="2"
                        />
                      ) : null;
                    })}
                    {shown.map((n) => {
                      const point = positions.get(n.id)!;
                      return (
                        <g
                          key={n.id}
                          tabIndex={0}
                          role="button"
                          aria-label={`Открыть заметку ${n.title}`}
                          onClick={() => onOpen(n.id)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              onOpen(n.id);
                            }
                          }}
                        >
                          <title>{n.title}</title>
                          <circle
                            cx={point.x}
                            cy={point.y}
                            r="19"
                            fill="#b092d3"
                          />
                          <text
                            x={point.x}
                            y={point.y + 35}
                            textAnchor="middle"
                            fontSize="11"
                            fill="#8b70a4"
                          >
                            {n.title.length > 23
                              ? n.title.slice(0, 21) + "…"
                              : n.title}
                          </text>
                        </g>
                      );
                    })}
                  </svg>
                  <p className="muted">
                    Нажмите узел, чтобы открыть заметку. Показано {shown.length}{" "}
                    из {graph.data.nodes.length} узлов.
                  </p>
                </>
              )}
            </>
          )}
        </>
      )}
    </section>
  );
}
function plain(value: string) {
  return value
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, "&");
}
