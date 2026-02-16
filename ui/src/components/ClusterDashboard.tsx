import React, { useState, useEffect, useCallback, useRef } from "react";

interface ClusterAgent {
  id: string;
  name: string;
  status: "idle" | "working" | "offline";
  current_task: string;
  capabilities: string[];
}

interface ClusterTask {
  id: string;
  title: string;
  status: "submitted" | "assigned" | "working" | "completed" | "failed";
  assigned_to: string;
  depends_on?: string[];
}

interface ClusterStatus {
  agents: ClusterAgent[];
  tasks: ClusterTask[];
  plan_summary: Record<string, number>;
}

const POLL_INTERVAL_MS = 5000;

const statusColors: Record<string, { bg: string; text: string; border: string }> = {
  idle: {
    bg: "var(--bg-tertiary)",
    text: "var(--text-secondary)",
    border: "var(--border)",
  },
  working: {
    bg: "var(--blue-bg)",
    text: "var(--blue-text)",
    border: "var(--blue-border)",
  },
  offline: {
    bg: "var(--error-bg)",
    text: "var(--error-text)",
    border: "var(--error-border)",
  },
  submitted: {
    bg: "var(--warning-bg)",
    text: "var(--warning-text)",
    border: "var(--warning-border)",
  },
  assigned: {
    bg: "var(--blue-bg)",
    text: "var(--blue-text)",
    border: "var(--blue-border)",
  },
  completed: {
    bg: "var(--success-bg)",
    text: "var(--success-text)",
    border: "var(--success-border)",
  },
  failed: {
    bg: "var(--error-bg)",
    text: "var(--error-text)",
    border: "var(--error-border)",
  },
};

function StatusBadge({ status }: { status: string }) {
  const colors = statusColors[status] || statusColors.idle;
  return (
    <span
      style={{
        display: "inline-block",
        fontSize: "0.625rem",
        fontWeight: 600,
        textTransform: "uppercase",
        letterSpacing: "0.05em",
        padding: "0.125rem 0.375rem",
        borderRadius: "0.25rem",
        background: colors.bg,
        color: colors.text,
        border: `1px solid ${colors.border}`,
        whiteSpace: "nowrap",
      }}
    >
      {status}
    </span>
  );
}

function ClusterDashboard() {
  const [status, setStatus] = useState<ClusterStatus | null>(null);
  const [notClusterMode, setNotClusterMode] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const intervalRef = useRef<number | null>(null);

  const fetchStatus = useCallback(async () => {
    try {
      const response = await fetch("/api/cluster/status");
      if (response.status === 404) {
        setNotClusterMode(true);
        return;
      }
      if (!response.ok) {
        return;
      }
      const data: ClusterStatus = await response.json();
      setStatus(data);
      setNotClusterMode(false);
    } catch {
      // Network error -- silently ignore, will retry
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    intervalRef.current = window.setInterval(fetchStatus, POLL_INTERVAL_MS);
    return () => {
      if (intervalRef.current !== null) {
        window.clearInterval(intervalRef.current);
      }
    };
  }, [fetchStatus]);

  if (notClusterMode) return null;
  if (!status) return null;

  const { agents, tasks, plan_summary } = status;

  if (collapsed) {
    return (
      <div
        style={{
          position: "fixed",
          right: 0,
          top: "50%",
          transform: "translateY(-50%)",
          zIndex: 40,
        }}
      >
        <button
          onClick={() => setCollapsed(false)}
          style={{
            background: "var(--bg-base)",
            border: "1px solid var(--border)",
            borderRight: "none",
            borderRadius: "0.375rem 0 0 0.375rem",
            padding: "0.5rem 0.25rem",
            cursor: "pointer",
            color: "var(--text-secondary)",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: "0.25rem",
          }}
          title="Show cluster dashboard"
          aria-label="Expand cluster dashboard"
        >
          <svg
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            style={{ width: "1rem", height: "1rem" }}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M15 19l-7-7 7-7"
            />
          </svg>
          <span
            style={{
              writingMode: "vertical-rl",
              fontSize: "0.625rem",
              fontWeight: 600,
              textTransform: "uppercase",
              letterSpacing: "0.1em",
            }}
          >
            Cluster
          </span>
        </button>
      </div>
    );
  }

  return (
    <div
      style={{
        width: "280px",
        flexShrink: 0,
        height: "100%",
        borderLeft: "1px solid var(--border)",
        background: "var(--bg-base)",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          padding: "0.75rem 1rem",
          borderBottom: "1px solid var(--border)",
          flexShrink: 0,
        }}
      >
        <span
          style={{
            fontSize: "0.75rem",
            fontWeight: 600,
            textTransform: "uppercase",
            letterSpacing: "0.05em",
            color: "var(--text-secondary)",
          }}
        >
          Cluster
        </span>
        <button
          onClick={() => setCollapsed(true)}
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            color: "var(--text-secondary)",
            padding: "0.25rem",
            display: "flex",
            alignItems: "center",
          }}
          title="Collapse cluster dashboard"
          aria-label="Collapse cluster dashboard"
        >
          <svg
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
            style={{ width: "1rem", height: "1rem" }}
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 5l7 7-7 7"
            />
          </svg>
        </button>
      </div>

      {/* Scrollable content */}
      <div style={{ flex: 1, overflowY: "auto", padding: "0.75rem" }}>
        {/* Summary counts */}
        {Object.keys(plan_summary).length > 0 && (
          <div style={{ marginBottom: "1rem" }}>
            <div
              style={{
                fontSize: "0.625rem",
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: "0.05em",
                color: "var(--text-tertiary)",
                marginBottom: "0.375rem",
              }}
            >
              Summary
            </div>
            <div
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: "0.375rem",
              }}
            >
              {Object.entries(plan_summary).map(([key, count]) => (
                <div
                  key={key}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: "0.25rem",
                    fontSize: "0.6875rem",
                    color: "var(--text-secondary)",
                  }}
                >
                  <span style={{ fontWeight: 600 }}>{count}</span>
                  <span>{key}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Agents */}
        {agents.length > 0 && (
          <div style={{ marginBottom: "1rem" }}>
            <div
              style={{
                fontSize: "0.625rem",
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: "0.05em",
                color: "var(--text-tertiary)",
                marginBottom: "0.375rem",
              }}
            >
              Agents ({agents.length})
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
              {agents.map((agent) => (
                <div
                  key={agent.id}
                  style={{
                    padding: "0.5rem 0.625rem",
                    borderRadius: "0.375rem",
                    border: "1px solid var(--border)",
                    background: "var(--bg-secondary)",
                  }}
                >
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      marginBottom: agent.current_task || agent.capabilities.length > 0 ? "0.25rem" : 0,
                    }}
                  >
                    <span
                      style={{
                        fontSize: "0.75rem",
                        fontWeight: 500,
                        color: "var(--text-primary)",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {agent.name}
                    </span>
                    <StatusBadge status={agent.status} />
                  </div>
                  {agent.current_task && (
                    <div
                      style={{
                        fontSize: "0.6875rem",
                        color: "var(--text-secondary)",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                      title={agent.current_task}
                    >
                      {agent.current_task}
                    </div>
                  )}
                  {agent.capabilities.length > 0 && (
                    <div
                      style={{
                        display: "flex",
                        flexWrap: "wrap",
                        gap: "0.25rem",
                        marginTop: "0.25rem",
                      }}
                    >
                      {agent.capabilities.map((cap) => (
                        <span
                          key={cap}
                          style={{
                            fontSize: "0.5625rem",
                            padding: "0.0625rem 0.25rem",
                            borderRadius: "0.1875rem",
                            background: "var(--bg-tertiary)",
                            color: "var(--text-tertiary)",
                          }}
                        >
                          {cap}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Tasks */}
        {tasks.length > 0 && (
          <div>
            <div
              style={{
                fontSize: "0.625rem",
                fontWeight: 600,
                textTransform: "uppercase",
                letterSpacing: "0.05em",
                color: "var(--text-tertiary)",
                marginBottom: "0.375rem",
              }}
            >
              Tasks ({tasks.length})
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: "0.375rem" }}>
              {tasks.map((task) => (
                <div
                  key={task.id}
                  style={{
                    padding: "0.5rem 0.625rem",
                    borderRadius: "0.375rem",
                    border: "1px solid var(--border)",
                    background: "var(--bg-secondary)",
                  }}
                >
                  <div
                    style={{
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                      marginBottom: task.assigned_to || (task.depends_on && task.depends_on.length > 0) ? "0.25rem" : 0,
                    }}
                  >
                    <span
                      style={{
                        fontSize: "0.75rem",
                        fontWeight: 500,
                        color: "var(--text-primary)",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        flex: 1,
                        marginRight: "0.375rem",
                      }}
                      title={task.title}
                    >
                      {task.title}
                    </span>
                    <StatusBadge status={task.status} />
                  </div>
                  {task.assigned_to && (
                    <div
                      style={{
                        fontSize: "0.6875rem",
                        color: "var(--text-secondary)",
                      }}
                    >
                      {task.assigned_to}
                    </div>
                  )}
                  {task.depends_on && task.depends_on.length > 0 && (
                    <div
                      style={{
                        fontSize: "0.625rem",
                        color: "var(--text-tertiary)",
                        marginTop: "0.125rem",
                      }}
                    >
                      depends on: {task.depends_on.join(", ")}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default ClusterDashboard;
