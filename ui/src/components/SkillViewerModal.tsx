import React, { useEffect, useState } from "react";
import Modal from "./Modal";
import MarkdownContent from "./MarkdownContent";
import { api, SkillContent } from "../services/api";

interface SkillViewerModalProps {
  name: string;
  cwd?: string;
  onClose: () => void;
}

function SkillViewerModal({ name, cwd, onClose }: SkillViewerModalProps) {
  const [data, setData] = useState<SkillContent | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .getSkillContent(name, cwd)
      .then((d) => {
        if (!cancelled) {
          setData(d);
          setError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) setError(err.message || String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [name, cwd]);

  return (
    <Modal isOpen onClose={onClose} title={name} className="skill-viewer-modal">
      {error && <p className="text-secondary">Failed to load: {error}</p>}
      {!error && !data && <p className="text-secondary">Loading…</p>}
      {data && (
        <>
          <p className="skill-viewer-path" title={data.path}>{data.path}</p>
          <div className="skill-viewer-content">
            <MarkdownContent text={data.content} />
          </div>
        </>
      )}
    </Modal>
  );
}

export default SkillViewerModal;
