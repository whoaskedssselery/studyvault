// The submitted HTML has its own URL and uses hash routes, so it can be hosted
// without server-side SPA rewriting, including in a subdirectory.
export const isSubmission = !!document.querySelector(
  'meta[name="studyvault-standalone"]',
);
export const apiBaseUrl = isSubmission || import.meta.env.DEV ? "/backend-api" : "/v1";
const modeKey = "studyvault.ui-mode";
function mode() {
  const requested = new URL(location.href).searchParams.get("mode");
  try {
    if (requested === "demo" || requested === "api")
      localStorage.setItem(modeKey, requested);
    return localStorage.getItem(modeKey) || (isSubmission ? "demo" : "api");
  } catch {
    return requested || (isSubmission ? "demo" : "api");
  }
}
export const demoMode = mode() === "demo";
export function mockUrl() {
  return isSubmission
    ? new URL("mock.json", window.location.href).href
    : "/mock.json";
}
