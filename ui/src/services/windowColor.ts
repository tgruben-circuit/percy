export type WindowColor = "default" | "orange" | "green";

const STORAGE_KEY = "percy-window-color";
const CLASS_NAMES: Record<Exclude<WindowColor, "default">, string> = {
  orange: "window-orange",
  green: "window-green",
};

export function getStoredWindowColor(): WindowColor {
  const stored = localStorage.getItem(STORAGE_KEY);
  if (stored === "orange" || stored === "green" || stored === "default") {
    return stored;
  }
  return "default";
}

export function setStoredWindowColor(color: WindowColor): void {
  localStorage.setItem(STORAGE_KEY, color);
}

export function applyWindowColor(color: WindowColor): void {
  const root = document.documentElement;
  for (const cls of Object.values(CLASS_NAMES)) {
    root.classList.remove(cls);
  }
  if (color !== "default") {
    root.classList.add(CLASS_NAMES[color]);
  }
}

export function initializeWindowColor(): void {
  applyWindowColor(getStoredWindowColor());
}
