import React, { useState, useEffect } from "react";
import { api } from "../services/api";

interface TouchedFile {
  path: string;
  operation: string;
  count: number;
}

interface FileTreePanelProps {
  conversationId: string;
  messageCount: number;
  onClose: () => void;
}

const OP_ICONS: Record<string, string> = {
  patch: "✏️",
  write: "✏️",
  read: "👁",
  navigate: "📁",
};

function FileTreePanel({ conversationId, messageCount, onClose }: FileTreePanelProps) {
  const [files, setFiles] = useState<TouchedFile[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!conversationId) return;
    setLoading(true);
    api.getTouchedFiles(conversationId).then(setFiles).catch(() => setFiles([])).finally(() => setLoading(false));
  }, [conversationId, messageCount]);

  return (
    <div style={{
      position: "absolute",
      right: 0,
      top: 0,
      bottom: 0,
      width: "300px",
      backgroundColor: "var(--bg-primary)",
      borderLeft: "1px solid var(--border-color)",
      display: "flex",
      flexDirection: "column",
      zIndex: 100,
      boxShadow: "-2px 0 8px rgba(0,0,0,0.1)",
    }}>
      <div style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        padding: "12px 16px",
        borderBottom: "1px solid var(--border-color)",
        fontWeight: 600,
        fontSize: "14px",
      }}>
        <span>Files ({files.length})</span>
        <button onClick={onClose} style={{
          background: "none", border: "none", cursor: "pointer",
          color: "var(--text-secondary)", fontSize: "18px", padding: "0 4px",
        }}>×</button>
      </div>
      <div style={{ flex: 1, overflowY: "auto", padding: "8px 0" }}>
        {loading ? (
          <div style={{ padding: "16px", textAlign: "center", color: "var(--text-secondary)" }}>Loading...</div>
        ) : files.length === 0 ? (
          <div style={{ padding: "16px", textAlign: "center", color: "var(--text-secondary)" }}>No files touched yet</div>
        ) : (
          files.map((file) => (
            <div key={file.path} style={{
              padding: "6px 16px",
              fontSize: "13px",
              display: "flex",
              alignItems: "center",
              gap: "8px",
              color: "var(--text-primary)",
            }}>
              <span style={{ flexShrink: 0 }}>{OP_ICONS[file.operation] || "📄"}</span>
              <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "var(--font-mono, monospace)", fontSize: "12px" }}>
                {file.path}
              </span>
              {file.count > 1 && (
                <span style={{
                  flexShrink: 0, fontSize: "11px", color: "var(--text-secondary)",
                  backgroundColor: "var(--bg-secondary)", borderRadius: "10px", padding: "1px 6px",
                }}>{file.count}</span>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

export default FileTreePanel;
