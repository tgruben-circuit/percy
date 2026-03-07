import React from "react";
import { linkifyText } from "./linkify";

interface TableData {
  headers: string[];
  alignments: ("left" | "center" | "right")[];
  rows: string[][];
}

/**
 * Parse a markdown table block into structured data.
 * Returns null if the lines don't form a valid markdown table.
 */
function parseTable(lines: string[]): TableData | null {
  if (lines.length < 2) return null;

  const parseCells = (line: string): string[] => {
    // Strip leading/trailing pipes and split
    let s = line.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|")) s = s.slice(0, -1);
    return s.split("|").map((c) => c.trim());
  };

  const headers = parseCells(lines[0]);
  if (headers.length === 0) return null;

  // Parse separator row for alignments
  const sepCells = parseCells(lines[1]);
  if (sepCells.length !== headers.length) return null;

  const alignments: ("left" | "center" | "right")[] = sepCells.map((cell) => {
    const s = cell.trim();
    // Must be dashes with optional colons
    if (!/^:?-+:?$/.test(s)) return "left";
    const left = s.startsWith(":");
    const right = s.endsWith(":");
    if (left && right) return "center";
    if (right) return "right";
    return "left";
  });

  // Validate separator row - all cells must be valid separators
  const validSep = sepCells.every((cell) => /^:?-+:?$/.test(cell.trim()));
  if (!validSep) return null;

  const rows: string[][] = [];
  for (let i = 2; i < lines.length; i++) {
    const cells = parseCells(lines[i]);
    // Pad or trim to match header count
    while (cells.length < headers.length) cells.push("");
    rows.push(cells.slice(0, headers.length));
  }

  return { headers, alignments, rows };
}

/**
 * Check if a line looks like it could be part of a markdown table.
 */
function isTableLine(line: string): boolean {
  const trimmed = line.trim();
  return trimmed.includes("|") && !trimmed.startsWith("```");
}

/**
 * Check if a line is a table separator (e.g., |---|---|---| or |:---:|---:|)
 */
function isSeparatorLine(line: string): boolean {
  const trimmed = line.trim();
  let s = trimmed;
  if (s.startsWith("|")) s = s.slice(1);
  if (s.endsWith("|")) s = s.slice(0, -1);
  const cells = s.split("|").map((c) => c.trim());
  return cells.length > 0 && cells.every((cell) => /^:?-+:?$/.test(cell));
}

interface Segment {
  type: "text" | "table";
  content: string; // for text
  table?: TableData; // for table
}

/**
 * Split text into segments of plain text and markdown tables.
 */
function segmentText(text: string): Segment[] {
  const lines = text.split("\n");
  const segments: Segment[] = [];
  let textLines: string[] = [];
  let inCodeBlock = false;

  const flushText = () => {
    if (textLines.length > 0) {
      segments.push({ type: "text", content: textLines.join("\n") });
      textLines = [];
    }
  };

  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    // Track code blocks - don't parse tables inside them
    if (line.trim().startsWith("```")) {
      inCodeBlock = !inCodeBlock;
      textLines.push(line);
      i++;
      continue;
    }

    if (inCodeBlock) {
      textLines.push(line);
      i++;
      continue;
    }

    // Look for table start: a line with pipes followed by a separator line
    if (
      isTableLine(line) &&
      i + 1 < lines.length &&
      isSeparatorLine(lines[i + 1])
    ) {
      // Collect table lines
      const tableLines: string[] = [line];
      let j = i + 1;
      // Add separator
      tableLines.push(lines[j]);
      j++;
      // Add data rows
      while (j < lines.length && isTableLine(lines[j]) && !isSeparatorLine(lines[j])) {
        tableLines.push(lines[j]);
        j++;
      }

      const table = parseTable(tableLines);
      if (table && table.rows.length > 0) {
        flushText();
        segments.push({ type: "table", content: "", table });
        i = j;
        continue;
      }
    }

    textLines.push(line);
    i++;
  }

  flushText();
  return segments;
}

/**
 * Render a TableData as a styled React table element.
 */
function TableRenderer({ table }: { table: TableData }) {
  return (
    <div className="md-table-wrapper">
      <table className="md-table">
        <thead>
          <tr>
            {table.headers.map((header, i) => (
              <th key={i} style={{ textAlign: table.alignments[i] }}>
                {header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {table.rows.map((row, ri) => (
            <tr key={ri}>
              {row.map((cell, ci) => (
                <td key={ci} style={{ textAlign: table.alignments[ci] }}>
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Format text content, rendering markdown tables as styled HTML tables
 * and everything else as linkified preformatted text.
 */
export function formatTextWithTables(text: string): React.ReactNode {
  const segments = segmentText(text);

  // Fast path: no tables found
  if (segments.length === 1 && segments[0].type === "text") {
    return linkifyText(text);
  }

  return segments.map((segment, i) => {
    if (segment.type === "table" && segment.table) {
      return <TableRenderer key={i} table={segment.table} />;
    }
    return (
      <React.Fragment key={i}>
        {linkifyText(segment.content)}
      </React.Fragment>
    );
  });
}
