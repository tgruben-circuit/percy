export type ThemeMode = "system" | "light" | "dark";

const STORAGE_KEY = "percy-theme";
const COLOR_THEME_STORAGE_KEY = "percy-color-theme";

export function getStoredTheme(): ThemeMode {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  return "system";
}

export function setStoredTheme(theme: ThemeMode): void {
  localStorage.setItem(STORAGE_KEY, theme);
}

export function getSystemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function isDarkModeActive(): boolean {
  const theme = getStoredTheme();
  return theme === "dark" || (theme === "system" && getSystemPrefersDark());
}

export function applyTheme(theme: ThemeMode): void {
  const isDark = theme === "dark" || (theme === "system" && getSystemPrefersDark());
  document.documentElement.classList.toggle("dark", isDark);
}

// ─── Color theme storage ─────────────────────────────────────────────────────

export function getStoredColorThemeId(): string | null {
  return localStorage.getItem(COLOR_THEME_STORAGE_KEY);
}

export function setStoredColorThemeId(id: string | null): void {
  if (id === null) {
    localStorage.removeItem(COLOR_THEME_STORAGE_KEY);
  } else {
    localStorage.setItem(COLOR_THEME_STORAGE_KEY, id);
  }
}

export function applyColorThemeVars(cssVars: Record<string, string> | null): void {
  const root = document.documentElement;
  if (!cssVars) {
    const knownVars = [
      "--bg-base", "--bg-secondary", "--bg-tertiary", "--bg-hover", "--bg-elevated",
      "--border", "--border-color", "--text-primary", "--text-secondary", "--text-tertiary",
      "--primary", "--primary-dark", "--user-message-text", "--link-color",
      "--success-bg", "--success-border", "--success-text",
      "--error-bg", "--error-border", "--error-text",
      "--warning-bg", "--warning-border", "--warning-text",
      "--blue-bg", "--blue-border", "--blue-text",
    ];
    for (const v of knownVars) {
      root.style.removeProperty(v);
    }
    return;
  }
  for (const [key, value] of Object.entries(cssVars)) {
    root.style.setProperty(key, value);
  }
}

// Initialize theme on load
export function initializeTheme(): void {
  const theme = getStoredTheme();
  applyTheme(theme);

  // Listen for system preference changes
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    const currentTheme = getStoredTheme();
    if (currentTheme === "system") {
      applyTheme("system");
    }
  });
}
