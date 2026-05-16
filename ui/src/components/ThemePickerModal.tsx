import React, { useState, useEffect, useRef } from "react";
import {
  ColorTheme,
  DARK_COLOR_THEMES,
  LIGHT_COLOR_THEMES,
  CATPPUCCIN_THEMES,
  HIGH_CONTRAST_THEMES,
} from "../services/colorThemes";

interface ThemePickerModalProps {
  currentThemeId: string | null;
  onSelect: (theme: ColorTheme | null) => void;
  onClose: () => void;
}

const ALL_DARK = [
  ...DARK_COLOR_THEMES,
  ...CATPPUCCIN_THEMES.filter((t) => t.type === "dark"),
  ...HIGH_CONTRAST_THEMES.filter((t) => t.type === "dark"),
];
const ALL_LIGHT = [
  ...LIGHT_COLOR_THEMES,
  ...CATPPUCCIN_THEMES.filter((t) => t.type === "light"),
  ...HIGH_CONTRAST_THEMES.filter((t) => t.type === "light"),
];

export default function ThemePickerModal({
  currentThemeId,
  onSelect,
  onClose,
}: ThemePickerModalProps) {
  const [tab, setTab] = useState<"dark" | "light">(() => {
    if (currentThemeId) {
      const isDark = ALL_DARK.some((t) => t.id === currentThemeId);
      return isDark ? "dark" : "light";
    }
    return document.documentElement.classList.contains("dark") ? "dark" : "light";
  });

  const modalRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  useEffect(() => {
    const handleClick = (e: MouseEvent) => {
      if (modalRef.current && !modalRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, [onClose]);

  const themes = tab === "dark" ? ALL_DARK : ALL_LIGHT;

  return (
    <div className="theme-picker-backdrop">
      <div className="theme-picker-modal" ref={modalRef}>
        <div className="theme-picker-header">
          <span className="theme-picker-title">Color Theme</span>
          <button className="theme-picker-close" onClick={onClose} aria-label="Close">
            <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
              <path d="M4.293 4.293a1 1 0 011.414 0L8 6.586l2.293-2.293a1 1 0 111.414 1.414L9.414 8l2.293 2.293a1 1 0 01-1.414 1.414L8 9.414l-2.293 2.293a1 1 0 01-1.414-1.414L6.586 8 4.293 5.707a1 1 0 010-1.414z" />
            </svg>
          </button>
        </div>

        <div className="theme-picker-tabs">
          <button
            className={`theme-picker-tab${tab === "dark" ? " active" : ""}`}
            onClick={() => setTab("dark")}
          >
            Dark
          </button>
          <button
            className={`theme-picker-tab${tab === "light" ? " active" : ""}`}
            onClick={() => setTab("light")}
          >
            Light
          </button>
        </div>

        <div className="theme-picker-grid">
          <button
            className={`theme-card theme-card-default${currentThemeId === null ? " selected" : ""}`}
            onClick={() => { onSelect(null); onClose(); }}
          >
            <div className="theme-card-swatch theme-card-swatch-default">
              <span>Aa</span>
            </div>
            <span className="theme-card-name">Default</span>
          </button>

          {themes.map((theme) => (
            <button
              key={theme.id}
              className={`theme-card${currentThemeId === theme.id ? " selected" : ""}`}
              onClick={() => { onSelect(theme); onClose(); }}
              title={theme.name}
            >
              <div
                className="theme-card-swatch"
                style={{ background: theme.previewBg }}
              >
                <div
                  className="theme-card-accent"
                  style={{ background: theme.previewAccent }}
                />
              </div>
              <span className="theme-card-name">{theme.name}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
