import type { Activity } from "./types";

export type TranscriptActivity = Activity & { detail_seqs?: number[] };

// Persisted fragments keep distinct seqs for history/SSE replay. Collapse only
// explicitly identified stream blocks; never merge unrelated answers or workers.
// A matching terminal result replaces its draft while keeping a stable UI key.
export function prepareTranscriptActivities(steps: Activity[]): TranscriptActivity[] {
  const result: TranscriptActivity[] = [];
  const blocks = new Map<string, number>();
  for (const step of steps) {
    const id = step.tool_use_id;
    if (!id?.startsWith("stream:") || !["text", "thinking", "result"].includes(step.kind)) {
      result.push(step);
      continue;
    }
    const key = `${step.worker}:${step.main_seg ?? 0}:${id}`;
    const index = blocks.get(key);
    if (index === undefined) {
      blocks.set(key, result.length);
      result.push({
        ...step,
        detail: step.kind === "result" ? step.detail : step.detail ?? step.summary,
        detail_seqs: [step.seq],
      });
      continue;
    }
    const previous = result[index];
    if (step.kind === "result") {
      result[index] = { ...step, seq: previous.seq, detail_seqs: [step.seq] };
    } else if (previous.kind !== "result" && previous.kind === step.kind) {
      result[index] = {
        ...previous,
        summary: previous.summary + step.summary,
        detail: (previous.detail ?? previous.summary) + (step.detail ?? step.summary),
        detail_seqs: [...(previous.detail_seqs ?? [previous.seq]), step.seq],
      };
    } else {
      result.push(step);
    }
  }
  return result;
}

export async function transcriptDetail(
  step: TranscriptActivity,
  getDetail: (seq: number) => Promise<string>,
): Promise<string> {
  if (step.detail !== undefined) return step.detail;
  const parts = await Promise.all((step.detail_seqs ?? [step.seq]).map(getDetail));
  return parts.join("");
}
