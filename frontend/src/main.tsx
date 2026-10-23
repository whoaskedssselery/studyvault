import React, { Component, Suspense, lazy } from "react";
import type { ErrorInfo, ReactNode } from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createRootRoute,
  createRoute,
  createRouter,
  createHashHistory,
  RouterProvider,
} from "@tanstack/react-router";
import { isSubmission, demoMode } from "./runtime";
import "./live/mail-token";
import "./styles.css";
import "./theme.css";
import "./live/styles.css";
import "./live/finish.css";

const DemoApp = lazy(() =>
  import("./demo-app").then((module) => ({ default: module.DemoApp })),
);
const LiveApp = lazy(() =>
  import("./live/app").then((module) => ({ default: module.LiveApp })),
);
const queryClient = new QueryClient();

function Opening() {
  return (
    <div className="app-opening" role="status">
      <span className="opening-mark">S</span>
      <strong>StudyVault</strong>
      <span>Открываем библиотеку…</span>
    </div>
  );
}
class ApplicationBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(_error: Error, _info: ErrorInfo) {
    /* The user can recover without exposing internal errors. */
  }
  render() {
    return this.state.failed ? (
      <div className="app-opening">
        <span className="opening-mark">S</span>
        <h1>Не удалось открыть страницу</h1>
        <p>
          Попробуйте обновить её. Ваши сохранённые материалы останутся на месте.
        </p>
        <button className="button primary" onClick={() => location.reload()}>
          Обновить страницу
        </button>
        <a className="text-button" href={isSubmission ? "./#/" : "/"}>
          Вернуться в библиотеку
        </a>
      </div>
    ) : (
      this.props.children
    );
  }
}
const rootRoute = createRootRoute({
  component: () => (
    <ApplicationBoundary>
      <Suspense fallback={<Opening />}>
        {demoMode ? <DemoApp /> : <LiveApp />}
      </Suspense>
    </ApplicationBoundary>
  ),
});
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
    "/api-status",
    "/workspace",
    "/authors/$authorId",
    "/forgot-password",
    "/reset-password",
    "/verify-email",
  ].map((path) => createRoute({ getParentRoute: () => rootRoute, path })),
);
const router = createRouter({
  routeTree,
  history: isSubmission ? createHashHistory() : undefined,
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
