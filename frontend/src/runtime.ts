// The submitted HTML has its own URL and uses hash routes, so it can be hosted
// without server-side SPA rewriting, including in a subdirectory.
export const isSubmission = !!document.querySelector('meta[name="studyvault-standalone"]');
export function mockUrl() { return isSubmission ? new URL('mock.json', window.location.href).href : '/mock.json'; }
