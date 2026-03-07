// markdownTable unit tests — run with: cd ui && pnpm run test:table

import { formatTextWithTables } from "./markdownTable";

// We test the internal segmenting logic indirectly through the export.
// Since formatTextWithTables returns ReactNode, we verify the table parser
// by re-exporting the segment function for testing.

// Test the parse logic directly by importing the segmentText function.
// We'll add a test export.

// For now, just do a basic smoke test that the function doesn't crash
// on various inputs.

const cases: { name: string; input: string; expectTable: boolean }[] = [
  {
    name: "plain text",
    input: "Hello world\nNo tables here.",
    expectTable: false,
  },
  {
    name: "simple table",
    input: `| Name | Age |
| --- | --- |
| Alice | 30 |
| Bob | 25 |`,
    expectTable: true,
  },
  {
    name: "table with alignment",
    input: `| Left | Center | Right |
| :--- | :---: | ---: |
| a | b | c |`,
    expectTable: true,
  },
  {
    name: "table embedded in text",
    input: `Here is a table:

| Col1 | Col2 |
| --- | --- |
| val1 | val2 |

And some text after.`,
    expectTable: true,
  },
  {
    name: "pipe in code block should not be a table",
    input: "```\n| not | a | table |\n| --- | --- | --- |\n| foo | bar | baz |\n```",
    expectTable: false,
  },
  {
    name: "single pipe line is not a table",
    input: "some | text | here",
    expectTable: false,
  },
];

let passed = 0;
let failed = 0;

for (const tc of cases) {
  try {
    const result = formatTextWithTables(tc.input);
    // If we expect a table, the result should be an array (multiple segments)
    // If no table, it could be a string or simple ReactNode
    const isArray = Array.isArray(result);
    if (tc.expectTable && !isArray) {
      console.error(`FAIL: ${tc.name} — expected table segments but got non-array`);
      failed++;
    } else if (!tc.expectTable && isArray) {
      console.error(`FAIL: ${tc.name} — expected plain text but got array`);
      failed++;
    } else {
      console.log(`PASS: ${tc.name}`);
      passed++;
    }
  } catch (e) {
    console.error(`FAIL: ${tc.name} — threw: ${e}`);
    failed++;
  }
}

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
