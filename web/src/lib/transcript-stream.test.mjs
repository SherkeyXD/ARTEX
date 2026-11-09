import assert from "node:assert/strict";
import test from "node:test";
import { prepareTranscriptActivities, transcriptDetail } from "./transcript-stream.ts";
import { mergeActivities } from "./activity-merge.ts";

const fragment = (seq, kind, text, id = "stream:run:1", extra = {}) => ({
  seq, kind, worker: "chat", ts: "2026-11-09T10:00:00Z", summary: text, tool_use_id: id, ...extra,
});

test("live fragments join without spaces or repeated answer bubbles", () => {
  const rows = prepareTranscriptActivities([
    fragment(1, "text", "Hello "), fragment(2, "text", "世界\n"), fragment(3, "text", "**done**"),
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].detail, "Hello 世界\n**done**");
  assert.equal(rows[0].seq, 1);
});

test("terminal result replaces the draft and lazily fetches its real seq", async () => {
  const rows = prepareTranscriptActivities([
    fragment(1, "text", "answer "), fragment(2, "text", "body"),
    fragment(3, "result", "answer body", "stream:run:1", { input_tokens: 10 }),
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].kind, "result");
  assert.equal(rows[0].seq, 1);
  const requested = [];
  assert.equal(await transcriptDetail(rows[0], async (seq) => { requested.push(seq); return "answer body"; }), "answer body");
  assert.deepEqual(requested, [3]);
  assert.equal(rows[0].input_tokens, 10);
});

test("thinking, intermediate text and different runs remain separate", () => {
  const rows = prepareTranscriptActivities([
    fragment(1, "thinking", "reason "), fragment(2, "thinking", "more"),
    fragment(3, "text", "working", "stream:run:2"),
    { seq: 4, kind: "tool_use", worker: "chat", summary: "tool" },
    fragment(5, "text", "answer", "stream:run:3"), fragment(6, "result", "answer", "stream:run:3"),
    fragment(7, "text", "other", "stream:other:1"),
  ]);
  assert.deepEqual(rows.map((row) => row.kind), ["thinking", "text", "tool_use", "result", "text"]);
  assert.equal(rows[0].detail, "reason more");
  assert.equal(rows[1].detail, "working");
});

test("overlapping polls do not duplicate streamed content", () => {
  const first = [fragment(1, "text", "a"), fragment(2, "text", "b")];
  const incoming = [fragment(2, "text", "b"), fragment(3, "text", "c")];
  const rows = prepareTranscriptActivities(mergeActivities(first, incoming));
  assert.equal(rows[0].detail, "abc");
});

test("failed terminal replaces partial output and retains the error detail", () => {
  const rows = prepareTranscriptActivities([
    fragment(1, "text", "partial"),
    fragment(2, "result", "partial\nerror", "stream:run:1", { is_error: true, detail: "partial\nerror" }),
  ]);
  assert.equal(rows.length, 1);
  assert.equal(rows[0].is_error, true);
  assert.equal(rows[0].detail, "partial\nerror");
});

test("legacy rows and distinct workers are never combined", () => {
  const input = [fragment(1, "text", "one", undefined, { tool_use_id: undefined }), fragment(2, "result", "two", undefined, { tool_use_id: undefined })];
  assert.deepEqual(prepareTranscriptActivities(input), input);
  assert.equal(prepareTranscriptActivities([fragment(1, "text", "a"), fragment(2, "text", "b", "stream:run:1", { worker: "other" })]).length, 2);
});
