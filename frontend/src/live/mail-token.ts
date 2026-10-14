// One-time mail tokens stay only in module memory, never in browser storage.
const landing = new URL(location.href);
const hashLanding = landing.hash.startsWith("#/")
  ? new URL(landing.hash.slice(1), landing.origin)
  : null;
const mailLanding = hashLanding || landing;
const route = mailLanding.pathname;
let verification =
  route === "/verify-email" ? mailLanding.searchParams.get("token") || "" : "";
let reset =
  route === "/reset-password"
    ? mailLanding.searchParams.get("token") || ""
    : "";
if (route === "/verify-email" || route === "/reset-password") {
  mailLanding.searchParams.delete("token");
  if (hashLanding) landing.hash = mailLanding.pathname + mailLanding.search;
  history.replaceState(
    history.state,
    "",
    hashLanding ? landing.href : mailLanding.href,
  );
}
export const mailToken = (kind: "verification" | "reset") =>
  kind === "verification" ? verification : reset;
export const clearMailToken = (kind: "verification" | "reset") => {
  if (kind === "verification") verification = "";
  else reset = "";
};
