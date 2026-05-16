import React, { useEffect, useState } from "react";
import { api, SkillSummary } from "../services/api";
import SkillViewerModal from "./SkillViewerModal";

interface SkillsListProps {
  cwd?: string;
}

function SkillsList({ cwd }: SkillsListProps) {
  const [skills, setSkills] = useState<SkillSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [viewing, setViewing] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    api
      .getSkills(cwd)
      .then((data) => {
        if (!cancelled) {
          setSkills([...data].sort((a, b) => a.name.localeCompare(b.name)));
          setError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) setError(err.message || String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [cwd]);

  if (loading) {
    return <div className="drawer-empty-state text-secondary"><p>Loading…</p></div>;
  }
  if (error) {
    return <div className="drawer-empty-state text-secondary"><p>Failed: {error}</p></div>;
  }
  if (skills.length === 0) {
    return (
      <div className="drawer-empty-state text-secondary">
        <p>No skills found.</p>
        <p className="skills-list-hint">
          Drop a <code>SKILL.md</code> into <code>~/.config/percy/skills/&lt;name&gt;/</code> or <code>.skills/&lt;name&gt;/</code>.
        </p>
      </div>
    );
  }

  return (
    <>
      <ul className="skills-list">
        {skills.map((s) => (
          <li key={`${s.scope}:${s.name}`}>
            <button type="button" className="skills-list-item" onClick={() => setViewing(s.name)}>
              <span className="skills-list-name">{s.name}</span>
              <span className="skills-list-desc">{s.description}</span>
              <span className={`skills-list-scope skills-list-scope-${s.scope}`}>{s.scope}</span>
            </button>
          </li>
        ))}
      </ul>
      {viewing && (
        <SkillViewerModal
          name={viewing}
          cwd={cwd}
          onClose={() => setViewing(null)}
        />
      )}
    </>
  );
}

export default SkillsList;
