import { useEffect, useState } from "react";

export type Theme = "light" | "dark";
const storageKey = "studyvault.theme.v1";
function readTheme(): Theme {
  try {
    return localStorage.getItem(storageKey) === "dark" ? "dark" : "light";
  } catch {
    return "light";
  }
}

// Apply the saved palette before React draws the interface.
document.documentElement.dataset.theme = readTheme();

export function useTheme() {
  const [theme, setTheme] = useState<Theme>(readTheme);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem(storageKey, theme);
    } catch {}
  }, [theme]);
  return { theme, toggleTheme: () => setTheme(value => value === "light" ? "dark" : "light") };
}
