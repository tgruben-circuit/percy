import React, { useState, useEffect } from "react";
import Modal from "./Modal";
import { api } from "../services/api";

interface CostDashboardProps {
  isOpen: boolean;
  onClose: () => void;
}

type Period = "7d" | "30d" | "90d" | "all";

interface UsageSummary {
  by_date: Array<{
    date: string;
    model: string | null;
    message_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_cost_usd: number;
  }>;
  by_conversation: Array<{
    conversation_id: string;
    slug: string | null;
    model: string | null;
    message_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_cost_usd: number;
  }>;
  total_cost_usd: number;
}

function getSinceDate(period: Period): string {
  if (period === "all") return "2000-01-01";
  const days = period === "7d" ? 7 : period === "30d" ? 30 : 90;
  const date = new Date();
  date.setDate(date.getDate() - days);
  return date.toISOString().split("T")[0];
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr + "T00:00:00");
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

function formatCost(cost: number): string {
  if (cost >= 1) return `$${cost.toFixed(2)}`;
  if (cost >= 0.01) return `$${cost.toFixed(4)}`;
  return `$${cost.toFixed(4)}`;
}

function formatTokens(n: number): string {
  return n.toLocaleString();
}

function CostDashboard({ isOpen, onClose }: CostDashboardProps) {
  const [period, setPeriod] = useState<Period>("30d");
  const [data, setData] = useState<UsageSummary | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) return;

    let cancelled = false;
    const fetchData = async () => {
      setLoading(true);
      setError(null);
      try {
        const since = getSinceDate(period);
        const result = await api.getUsage(since);
        if (!cancelled) {
          setData(result);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Failed to load usage data");
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    fetchData();
    return () => {
      cancelled = true;
    };
  }, [isOpen, period]);

  const periods: { value: Period; label: string }[] = [
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
    { value: "90d", label: "90d" },
    { value: "all", label: "All time" },
  ];

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Usage & Costs" className="modal-wide">
      <div className="models-modal">
        {/* Period selector */}
        <div className="form-group">
          <div className="provider-buttons">
            {periods.map((p) => (
              <button
                key={p.value}
                type="button"
                className={`provider-btn ${period === p.value ? "selected" : ""}`}
                onClick={() => setPeriod(p.value)}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>

        {error && (
          <div className="models-error">
            {error}
            <button onClick={() => setError(null)} className="models-error-dismiss">
              ×
            </button>
          </div>
        )}

        {loading ? (
          <div className="models-loading">
            <div className="spinner"></div>
            <span>Loading usage data...</span>
          </div>
        ) : data ? (
          <>
            {/* Total spend */}
            <div style={{ textAlign: "center", margin: "1rem 0 1.5rem" }}>
              <div style={{ fontSize: "2rem", fontWeight: 700 }}>{formatCost(data.total_cost_usd)}</div>
              <div className="text-secondary" style={{ fontSize: "0.85rem" }}>
                Total spend
              </div>
            </div>

            {/* By Date table */}
            {data.by_date.length > 0 && (
              <div style={{ marginBottom: "1.5rem" }}>
                <h3 style={{ margin: "0 0 0.5rem", fontSize: "0.95rem" }}>By Date</h3>
                <div style={{ overflowX: "auto" }}>
                  <table className="cost-table">
                    <thead>
                      <tr>
                        <th>Date</th>
                        <th>Model</th>
                        <th style={{ textAlign: "right" }}>Messages</th>
                        <th style={{ textAlign: "right" }}>Input Tokens</th>
                        <th style={{ textAlign: "right" }}>Output Tokens</th>
                        <th style={{ textAlign: "right" }}>Cost</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.by_date.map((row, i) => (
                        <tr key={i}>
                          <td>{formatDate(row.date)}</td>
                          <td className="text-secondary">{row.model || "—"}</td>
                          <td style={{ textAlign: "right" }}>{row.message_count}</td>
                          <td style={{ textAlign: "right" }}>{formatTokens(row.total_input_tokens)}</td>
                          <td style={{ textAlign: "right" }}>{formatTokens(row.total_output_tokens)}</td>
                          <td style={{ textAlign: "right" }}>{formatCost(row.total_cost_usd)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {/* By Conversation table */}
            {data.by_conversation.length > 0 && (
              <div>
                <h3 style={{ margin: "0 0 0.5rem", fontSize: "0.95rem" }}>By Conversation</h3>
                <div style={{ overflowX: "auto" }}>
                  <table className="cost-table">
                    <thead>
                      <tr>
                        <th>Conversation</th>
                        <th>Model</th>
                        <th style={{ textAlign: "right" }}>Messages</th>
                        <th style={{ textAlign: "right" }}>Cost</th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.by_conversation.map((row) => (
                        <tr key={row.conversation_id}>
                          <td
                            style={{
                              maxWidth: "200px",
                              overflow: "hidden",
                              textOverflow: "ellipsis",
                              whiteSpace: "nowrap",
                            }}
                            title={row.slug || row.conversation_id}
                          >
                            {row.slug || row.conversation_id}
                          </td>
                          <td className="text-secondary">{row.model || "—"}</td>
                          <td style={{ textAlign: "right" }}>{row.message_count}</td>
                          <td style={{ textAlign: "right" }}>{formatCost(row.total_cost_usd)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            )}

            {data.by_date.length === 0 && data.by_conversation.length === 0 && (
              <div className="models-empty">
                <p>No usage data for the selected period.</p>
              </div>
            )}
          </>
        ) : null}
      </div>
    </Modal>
  );
}

export default CostDashboard;
