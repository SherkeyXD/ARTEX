"use client";

import * as React from "react";

import Link from "next/link";

import {
  ActivityIcon,
  ArrowUpRightIcon,
  BotIcon,
  BrainIcon,
  BugIcon,
  ChevronRightIcon,
  ClockIcon,
  GaugeIcon,
  LayersIcon,
  NetworkIcon,
  ShieldCheckIcon,
  TargetIcon,
  TerminalIcon,
  WrenchIcon,
  ZapIcon,
} from "lucide-react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from "recharts";

import { StatusBadge } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { type ChartConfig, ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Progress } from "@/components/ui/progress";
import { useVisibilityPoll } from "@/hooks/use-visibility-poll";
import { api } from "@/lib/api";
import type {
  Activity,
  Agent,
  ConvTokenSummary,
  Finding,
  GlobalContextMetricsReport,
  InterceptPending,
  LLMProfile,
  MCPServer,
  Settings,
  SkillItem,
  Stats,
  Task,
  TokenTotal,
  Tool,
  UsageStats,
} from "@/lib/types";
import { cn } from "@/lib/utils";

// ── chart constants ───────────────────────────────────────────────────────────

const dailyTrendConfig = {
  input: { label: "输入", color: "hsl(217 91% 60%)" },
  output: { label: "输出", color: "hsl(263 70% 60%)" },
  cacheRead: { label: "缓存读", color: "hsl(160 60% 45%)" },
} satisfies ChartConfig;

// ── helpers ──────────────────────────────────────────────────────────────────

function fmtRel(ts?: string | number): string {
  if (!ts) return "—";
  const ms = Date.now() - (typeof ts === "number" ? ts * 1000 : Date.parse(ts as string));
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s 前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m 前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h 前`;
  return `${Math.floor(h / 24)}d 前`;
}

function fmtMs(ms: number): string {
  if (ms <= 0) return "0ms";
  if (ms < 1000) return `${ms}ms`;
  const sec = ms / 1000;
  if (sec < 60) return `${sec.toFixed(1)}s`;
  const min = Math.floor(sec / 60);
  const remSec = Math.round(sec % 60);
  return `${min}m ${remSec}s`;
}

function fmtTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + "M";
  if (n >= 1000) return (n / 1000).toFixed(1) + "k";
  return String(n);
}

const ASSET_TYPE_LABELS: Record<string, string> = {
  root_domain: "根域名",
  ip: "IP",
  subdomain: "子域名",
  app: "应用",
  service: "服务",
  endpoint: "端点",
};

const ASSET_COLORS: Record<string, string> = {
  root_domain: "bg-blue-500",
  ip: "bg-indigo-400",
  subdomain: "bg-violet-500",
  app: "bg-orange-400",
  service: "bg-cyan-500",
  endpoint: "bg-emerald-500",
};

function statusColor(code: number): string {
  if (code < 300) return "text-emerald-500";
  if (code < 400) return "text-blue-400";
  if (code < 500) return "text-amber-400";
  return "text-red-400";
}

function statusBg(code: number): string {
  if (code < 300) return "bg-emerald-500";
  if (code < 400) return "bg-blue-400";
  if (code < 500) return "bg-amber-400";
  return "bg-red-400";
}

// ── sub-components ────────────────────────────────────────────────────────────

function LiveDot({ className }: { className?: string }) {
  return <span className={cn("inline-block size-1.5 shrink-0 animate-pulse rounded-full bg-blue-400", className)} />;
}

function SectionTitle({
  icon: Icon,
  children,
  sub,
}: {
  icon: React.ElementType;
  children: React.ReactNode;
  sub?: React.ReactNode;
}) {
  return (
    <div className="mb-3 flex items-center justify-between">
      <div className="flex items-center gap-1.5 text-xs font-semibold">
        <Icon className="size-3.5 text-muted-foreground" />
        {children}
      </div>
      {sub && <span className="text-[10px] text-muted-foreground">{sub}</span>}
    </div>
  );
}

// ── page ─────────────────────────────────────────────────────────────────────

export default function DashboardPage() {
  // data state
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [findings, setFindings] = React.useState<Finding[]>([]);
  const [stats, setStats] = React.useState<Stats | null>(null);
  const [settings, setSettings] = React.useState<Settings | null>(null);
  const [pending, setPending] = React.useState<InterceptPending[]>([]);
  const [activity, setActivity] = React.useState<Activity[]>([]);
  const [tokens, setTokens] = React.useState<TokenTotal | null>(null);
  const [convTokens, setConvTokens] = React.useState<ConvTokenSummary[]>([]);
  const [usageStats, setUsageStats] = React.useState<UsageStats | null>(null);
  const [assetCounts, setAssetCounts] = React.useState<Record<string, number>>({});
  const [agents, setAgents] = React.useState<Agent[]>([]);
  const [mcpServers, setMcpServers] = React.useState<MCPServer[]>([]);
  const [skills, setSkills] = React.useState<SkillItem[]>([]);
  const [tools, setTools] = React.useState<Tool[]>([]);
  const [llmProfiles, setLLMProfiles] = React.useState<LLMProfile[]>([]);
  const [contextMetrics, setContextMetrics] = React.useState<GlobalContextMetricsReport | null>(null);

  // The task list is the most expensive dashboard source. Poll it independently
  // when the page is visible so a large history cannot hold back every other dashboard panel.
  const taskSigRef = React.useRef("");
  useVisibilityPoll(async () => {
    try {
      const tr = await api.tasks();
      const nextSignature = JSON.stringify(tr.tasks);
      if (nextSignature !== taskSigRef.current) {
        taskSigRef.current = nextSignature;
        setTasks(tr.tasks);
      }
    } catch {
      /* transient errors — next poll retries */
    }
  }, 5000);

  // The remaining fast sources stay batched with dirty checking so unchanged polls do not re-render.
  const fastSigRef = React.useRef("");
  useVisibilityPoll(async () => {
    try {
      const [findings, stats, settings, pending, activity, tokens, conversationTokens] = await Promise.all([
        api.findings(),
        api.stats(),
        api.settings(),
        api.interceptPending(),
        api.activity(undefined, { limit: 30 }),
        api.tokenStats(),
        api.conversationTokens(),
      ]);
      const nextSig = JSON.stringify([
        findings.length,
        findings[0]?.id,
        findings[0]?.ts,
        stats,
        settings,
        pending.length,
        activity.items[0]?.seq,
        activity.items.length,
        tokens.total,
        conversationTokens,
      ]);
      if (nextSig === fastSigRef.current) return;
      fastSigRef.current = nextSig;
      setFindings(findings);
      setStats(stats);
      setSettings(settings);
      setPending(pending);
      setActivity(activity.items);
      setTokens(tokens.total ?? null);
      setConvTokens(conversationTokens);
    } catch {
      // Preserve stale data and retry on the next interval.
    }
  }, 5000);

  // slow poll: assets, system-static (every 15s) with dirty checking
  const slowSigRef = React.useRef("");
  useVisibilityPoll(async () => {
    try {
      const [counts, agentList, mcpList, skillList, toolList, profileList, usage] = await Promise.all([
        api.assetCounts(),
        api.agents(),
        api.mcpServers(),
        api.skills(),
        api.tools(),
        api.llmProfiles(),
        api.usageStats(365),
      ]);
      const nextSig = JSON.stringify([
        counts,
        agentList.length,
        mcpList.length,
        skillList.length,
        toolList.length,
        profileList.length,
        usage?.daily?.length,
        usage?.by_profile?.length,
      ]);
      if (nextSig === slowSigRef.current) return;
      slowSigRef.current = nextSig;
      setAssetCounts(counts ?? {});
      setAgents(agentList);
      setMcpServers(mcpList);
      setSkills(skillList);
      setTools(toolList);
      setLLMProfiles(profileList);
      setUsageStats(usage);
    } catch {
      /* transient errors */
    }
  }, 15000);

  // Context metrics poll (every 10s)
  const contextSigRef = React.useRef("");
  useVisibilityPoll(async () => {
    try {
      const rep = await api.globalContextMetrics(tokenDays);
      const sig = JSON.stringify(rep);
      if (sig === contextSigRef.current) return;
      contextSigRef.current = sig;
      setContextMetrics(rep);
    } catch {
      /* transient errors */
    }
  }, 10000);

  // ── derived ───────────────────────────────────────────────────────────────

  const tasksByStatus = React.useMemo(() => {
    const m: Record<string, number> = {};
    for (const t of tasks) m[t.status] = (m[t.status] ?? 0) + 1;
    return m;
  }, [tasks]);

  const findingsBySev = React.useMemo(() => {
    const m = { critical: 0, high: 0, medium: 0, low: 0 };
    for (const f of findings) {
      if (f.severity in m) m[f.severity as keyof typeof m]++;
    }
    return m;
  }, [findings]);

  const sortedTasks = React.useMemo(
    () =>
      [...tasks]
        .sort((a, b) => (b.last_activity_unix ?? b.created_unix ?? 0) - (a.last_activity_unix ?? a.created_unix ?? 0))
        .slice(0, 5),
    [tasks],
  );

  const recentFindings = React.useMemo(
    () => [...findings].sort((a, b) => Date.parse(b.ts) - Date.parse(a.ts)).slice(0, 8),
    [findings],
  );

  const recentActivity = React.useMemo(
    () =>
      [...activity]
        .filter((a) => a.kind !== "usage")
        .sort((a, b) => b.seq - a.seq)
        .slice(0, 6),
    [activity],
  );

  const totalAssets = React.useMemo(() => Object.values(assetCounts).reduce((a, b) => a + b, 0), [assetCounts]);

  // asset type breakdown
  const assetByType = React.useMemo(() => {
    return Object.entries(assetCounts)
      .filter(([, n]) => n > 0)
      .sort(([, a], [, b]) => b - a)
      .slice(0, 8);
  }, [assetCounts]);

  const assetMax = assetByType[0]?.[1] ?? 1;

  // system
  const activeProfile = llmProfiles.find((p) => p.is_default);
  const enabledTools = tools.filter((t) => t.enabled);
  const pendingCount = pending.length;

  // ── token stats per LLM profile ──────────────────────────────────────────
  // tasks whose llm_profile_id is null/undefined used the active default profile
  const defaultProfileId = activeProfile ? Number(activeProfile.id) : null;

  // selected profile tab: "all" = 全部; number = specific profile id
  const [tokenTab, setTokenTab] = React.useState<number | null | "all">("all");
  // day range for the daily bar chart
  const [tokenDays, setTokenDays] = React.useState<7 | 30 | 90 | 180 | 365>(30);

  // profile 名 → id，用于把 llm_usage 的 profile_name 映射到现有 profile 分栏。
  const profileIdByName = React.useMemo(() => {
    const m = new Map<string, number>();
    for (const p of llmProfiles) m.set(p.name, Number(p.id));
    return m;
  }, [llmProfiles]);

  type Bucket = { input: number; output: number; cacheRead: number; cacheWrite: number; taskCount: number };

  // 旧版：按 profile 归桶（来自 activity 的 task.tokens + 会话用量）。
  const tokenByProfileOld = React.useMemo<Map<number | null, Bucket>>(() => {
    const m = new Map<number | null, Bucket>();
    const fold = (key: number | null, inp: number, out: number, cr: number, cw: number, addTask: boolean) => {
      const prev = m.get(key) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, taskCount: 0 };
      m.set(key, {
        input: prev.input + inp,
        output: prev.output + out,
        cacheRead: prev.cacheRead + cr,
        cacheWrite: prev.cacheWrite + cw,
        taskCount: prev.taskCount + (addTask ? 1 : 0),
      });
    };
    for (const t of tasks) {
      fold(
        t.llm_profile_id ?? defaultProfileId,
        t.tokens?.input_tokens ?? 0,
        t.tokens?.output_tokens ?? 0,
        t.tokens?.cache_read_tokens ?? 0,
        t.tokens?.cache_write_tokens ?? 0,
        true,
      );
    }
    for (const c of convTokens) {
      fold(
        c.llm_profile_id ?? defaultProfileId,
        c.input_tokens,
        c.output_tokens,
        c.cache_read_tokens,
        c.cache_write_tokens,
        false,
      );
    }
    return m;
  }, [tasks, convTokens, defaultProfileId]);

  // 新版：按 profile 归桶（来自 llm_usage 全局聚合，逐次调用精确）。
  const tokenByProfileNew = React.useMemo<Map<number | null, Bucket>>(() => {
    const m = new Map<number | null, Bucket>();
    for (const p of usageStats?.by_profile ?? []) {
      // 未匹配到现有 profile（改名/删除/空名）→ 落到默认桶，仍计入「全部」。
      const key = profileIdByName.get(p.profile_name) ?? defaultProfileId;
      const prev = m.get(key) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, taskCount: 0 };
      m.set(key, {
        input: prev.input + p.input_tokens,
        output: prev.output + p.output_tokens,
        cacheRead: prev.cacheRead + p.cache_read_tokens,
        cacheWrite: prev.cacheWrite + p.cache_write_tokens,
        taskCount: prev.taskCount + p.tasks,
      });
    }
    return m;
  }, [usageStats, profileIdByName, defaultProfileId]);

  const tokenByProfile = (usageStats?.by_profile?.length ?? 0) > 0 ? tokenByProfileNew : tokenByProfileOld;

  const displayedTokens = React.useMemo(() => {
    if (tokenTab === "all") {
      let input = 0,
        output = 0,
        cacheRead = 0,
        cacheWrite = 0,
        taskCount = 0;
      for (const v of tokenByProfile.values()) {
        input += v.input;
        output += v.output;
        cacheRead += v.cacheRead;
        cacheWrite += v.cacheWrite;
        taskCount += v.taskCount;
      }
      return { input, output, cacheRead, cacheWrite, taskCount };
    }
    const v = tokenByProfile.get(tokenTab as number | null);
    return v
      ? { input: v.input, output: v.output, cacheRead: v.cacheRead, cacheWrite: v.cacheWrite, taskCount: v.taskCount }
      : { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, taskCount: 0 };
  }, [tokenTab, tokenByProfile]);

  // 旧版每日：把任务/会话的总量按其创建日期归桶（近似，非真实每日消耗）。
  const dailyTokenDataOld = React.useMemo(() => {
    const now = new Date();
    now.setDate(now.getDate() - tokenDays);
    const cutoff = now.toISOString().slice(0, 10);
    const m = new Map<string, { input: number; output: number; cacheRead: number }>();
    const fold = (date: string, inp: number, out: number, cr: number) => {
      if (date < cutoff) return;
      const prev = m.get(date) ?? { input: 0, output: 0, cacheRead: 0 };
      m.set(date, { input: prev.input + inp, output: prev.output + out, cacheRead: prev.cacheRead + cr });
    };
    for (const t of tasks) {
      if (tokenTab !== "all" && (t.llm_profile_id ?? defaultProfileId) !== tokenTab) continue;
      fold(
        t.created_at.slice(0, 10),
        t.tokens?.input_tokens ?? 0,
        t.tokens?.output_tokens ?? 0,
        t.tokens?.cache_read_tokens ?? 0,
      );
    }
    for (const c of convTokens) {
      if (tokenTab !== "all" && (c.llm_profile_id ?? defaultProfileId) !== tokenTab) continue;
      fold(c.created_at.slice(0, 10), c.input_tokens, c.output_tokens, c.cache_read_tokens);
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([date, v]) => ({ date: date.slice(5), ...v }));
  }, [tasks, convTokens, tokenDays, tokenTab, defaultProfileId]);

  // 新版每日：来自 llm_usage 的真实每日消耗（ts 是实际调用时刻）。
  const dailyTokenDataNew = React.useMemo(() => {
    const now = new Date();
    now.setDate(now.getDate() - tokenDays);
    const cutoff = now.toISOString().slice(0, 10);
    const m = new Map<string, { input: number; output: number; cacheRead: number }>();
    for (const d of usageStats?.daily ?? []) {
      if (d.date < cutoff) continue;
      if (tokenTab !== "all" && (profileIdByName.get(d.profile_name) ?? defaultProfileId) !== tokenTab) continue;
      const prev = m.get(d.date) ?? { input: 0, output: 0, cacheRead: 0 };
      m.set(d.date, {
        input: prev.input + d.input_tokens,
        output: prev.output + d.output_tokens,
        cacheRead: prev.cacheRead + d.cache_read_tokens,
      });
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([date, v]) => ({ date: date.slice(5), ...v }));
  }, [usageStats, tokenDays, tokenTab, profileIdByName, defaultProfileId]);

  const dailyTokenData = (usageStats?.daily?.length ?? 0) > 0 ? dailyTokenDataNew : dailyTokenDataOld;

  // activity kind label
  function kindLabel(a: Activity): string {
    if (a.kind === "tool_use") return a.tool ?? "tool_use";
    if (a.kind === "tool_result") return "tool_result";
    return a.kind;
  }

  function workerColor(w: string): string {
    if (w === "planner") return "text-violet-400";
    if (w === "mainagent") return "text-cyan-400";
    return "text-blue-400";
  }

  function workerBg(w: string): string {
    if (w === "planner") return "bg-violet-500/10 text-violet-400 border-violet-500/20";
    if (w === "mainagent") return "bg-cyan-500/10 text-cyan-400 border-cyan-500/20";
    return "bg-blue-500/10 text-blue-400 border-blue-500/20";
  }

  // ── render ────────────────────────────────────────────────────────────────

  return (
    <div className="flex flex-1 flex-col gap-4 pb-6">
      {/* ── Header ── */}
      <div>
        <div>
          <h1 className="text-lg font-semibold tracking-tight">总览</h1>
          <p className="text-xs text-muted-foreground">系统全局状态 · 实时刷新</p>
        </div>
      </div>

      {/* ── Row 1: 4 stat cards ── */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {/* 活跃任务 */}
        <Card className="gap-1">
          <CardHeader className="pb-0">
            <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <TargetIcon className="size-3" /> 活跃任务
            </div>
            <div className="flex items-baseline gap-1.5">
              <span className="text-2xl font-semibold tabular-nums">{tasksByStatus.running ?? 0}</span>
              <span className="text-xs text-muted-foreground">/ {tasks.length}</span>
            </div>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-x-2.5 gap-y-0.5 text-[10px]">
            {(tasksByStatus.running ?? 0) > 0 && <span className="text-blue-400">探索 {tasksByStatus.running}</span>}
            {(tasksByStatus.paused ?? 0) > 0 && <span className="text-amber-400">暂停 {tasksByStatus.paused}</span>}
            {(tasksByStatus.done ?? 0) > 0 && <span className="text-emerald-400">完成 {tasksByStatus.done}</span>}
            {tasks.length === 0 && <span className="text-muted-foreground">暂无任务</span>}
          </CardContent>
        </Card>

        {/* 确认发现 */}
        <Card className="gap-1">
          <CardHeader className="pb-0">
            <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <BugIcon className="size-3" /> 确认发现
            </div>
            <div className="text-2xl font-semibold tabular-nums">{findings.length}</div>
          </CardHeader>
          <CardContent className="flex flex-wrap gap-2.5 text-[10px]">
            <span className="text-rose-500">严重 {findingsBySev.critical}</span>
            <span className="text-red-400">高危 {findingsBySev.high}</span>
            <span className="text-amber-400">中危 {findingsBySev.medium}</span>
            <span className="text-slate-400">低危 {findingsBySev.low}</span>
          </CardContent>
        </Card>

        {/* 资产节点 */}
        <Card className="gap-1">
          <CardHeader className="pb-0">
            <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <NetworkIcon className="size-3" /> 资产节点
            </div>
            <div className="text-2xl font-semibold tabular-nums">{totalAssets}</div>
          </CardHeader>
          <CardContent className="text-[10px] text-muted-foreground">跨任务共享</CardContent>
        </Card>

        {/* LLM 用量 */}
        <Card className="gap-1">
          <CardHeader className="pb-0">
            <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
              <ZapIcon className="size-3" /> Token 用量
            </div>
            <div className="text-2xl font-semibold tabular-nums">
              {fmtTokens(displayedTokens.input + displayedTokens.output) || "—"}
            </div>
          </CardHeader>
          <CardContent className="text-[10px] text-muted-foreground">
            入 {fmtTokens(displayedTokens.input)}（含缓存 {fmtTokens(displayedTokens.cacheRead)}）· 出{" "}
            {fmtTokens(displayedTokens.output)}
          </CardContent>
        </Card>
      </div>

      {/* ── Row 2: LLM Token 用量与步骤耗时性能看板 ── */}
      <Card className="p-4">
        {/* Header */}
        <div className="mb-4 flex flex-wrap items-center justify-between gap-2 border-b pb-3">
          <div className="flex items-center gap-1.5 text-xs font-semibold">
            <BrainIcon className="size-4 text-violet-500" />
            LLM Token 用量与步骤耗时透视
          </div>

          <div className="flex items-center gap-2">
            {/* Range selector */}
            <div className="flex gap-0.5 rounded-md border bg-muted/30 p-0.5">
              {(
                [
                  { days: 7, label: "7天" },
                  { days: 30, label: "30天" },
                  { days: 90, label: "3月" },
                  { days: 180, label: "6月" },
                  { days: 365, label: "一年" },
                ] as const
              ).map(({ days, label }) => (
                <button
                  type="button"
                  key={days}
                  onClick={() => setTokenDays(days)}
                  className={cn(
                    "rounded px-2.5 py-0.5 text-[9px] font-medium transition-colors",
                    tokenDays === days
                      ? "bg-background text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {label}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="space-y-4">
          {/* Part 1: KPI Ribbon (6 维度指标卡) */}
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <LayersIcon className="size-3 text-blue-400" /> 总上下文 Tokens
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums">
                {fmtTokens(contextMetrics?.summary.total_tokens ?? (displayedTokens.input + displayedTokens.output))}
              </div>
              <div className="text-[9px] text-muted-foreground">
                入 {fmtTokens(contextMetrics?.summary.input_tokens ?? displayedTokens.input)} · 出{" "}
                {fmtTokens(contextMetrics?.summary.output_tokens ?? displayedTokens.output)}
              </div>
            </div>

            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <ZapIcon className="size-3 text-emerald-400" /> Prompt 缓存命中率
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums text-emerald-400">
                {contextMetrics && (contextMetrics.summary.input_tokens > 0 || contextMetrics.summary.cache_read_tokens > 0)
                  ? `${Math.round((contextMetrics.summary.cache_hit_rate ?? 0) * 100)}%`
                  : displayedTokens.input > 0
                    ? `${Math.round((displayedTokens.cacheRead / displayedTokens.input) * 100)}%`
                    : "0%"}
              </div>
              <div className="text-[9px] text-muted-foreground">
                命中 {fmtTokens(contextMetrics?.summary.cache_read_tokens ?? displayedTokens.cacheRead)}
              </div>
            </div>

            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <BrainIcon className="size-3 text-violet-400" /> 思考耗时 (Thinking)
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums text-violet-400">
                {fmtMs(contextMetrics?.summary.thinking_duration_ms ?? 0)}
              </div>
              <div className="text-[9px] text-muted-foreground">模型深度推理阶段</div>
            </div>

            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <WrenchIcon className="size-3 text-amber-400" /> 工具耗时 (Tools)
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums text-amber-400">
                {fmtMs(contextMetrics?.summary.tool_duration_ms ?? 0)}
              </div>
              <div className="text-[9px] text-muted-foreground">
                调用 {contextMetrics?.summary.tool_calls ?? 0} 次
              </div>
            </div>

            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <ClockIcon className="size-3 text-cyan-400" /> LLM 生成耗时
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums text-cyan-400">
                {fmtMs(contextMetrics?.summary.llm_duration_ms ?? 0)}
              </div>
              <div className="text-[9px] text-muted-foreground">
                调用 {contextMetrics?.summary.llm_calls ?? 0} 次
              </div>
            </div>

            <div className="rounded-lg border bg-muted/15 p-2.5">
              <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
                <GaugeIcon className="size-3 text-pink-400" /> 生成吞吐速度
              </div>
              <div className="mt-1 text-lg font-bold tabular-nums text-pink-400">
                {(contextMetrics?.summary.avg_tokens_per_sec ?? 0) > 0
                  ? `${(contextMetrics?.summary.avg_tokens_per_sec ?? 0).toFixed(1)} t/s`
                  : "—"}
              </div>
              <div className="text-[9px] text-muted-foreground">平均出词速率</div>
            </div>
          </div>

          {/* Part 2: 核心分析区 (4 象限网格) */}
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {/* 象限 1: 上下文构成拆解 */}
            <div className="flex flex-col rounded-lg border bg-muted/10 p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[11px] font-semibold flex items-center gap-1.5">
                  <LayersIcon className="size-3.5 text-muted-foreground" /> 上下文构成拆解 (Tokens)
                </span>
                <span className="text-[9px] text-muted-foreground">按类型占比</span>
              </div>

              <div className="flex items-center gap-4">
                <div className="h-[120px] w-[120px] shrink-0">
                  <PieChart width={120} height={120}>
                    <Pie
                      data={
                        (contextMetrics?.composition.categories.length ?? 0) > 0 &&
                        contextMetrics!.composition.categories.some((c) => c.tokens > 0)
                          ? contextMetrics!.composition.categories.filter((c) => c.tokens > 0)
                          : [{ name: "暂无数据", tokens: 1, color: "hsl(var(--muted))" }]
                      }
                      dataKey="tokens"
                      nameKey="name"
                      cx="50%"
                      cy="50%"
                      innerRadius={30}
                      outerRadius={50}
                      paddingAngle={2}
                      isAnimationActive={false}
                    >
                      {(
                        (contextMetrics?.composition.categories.length ?? 0) > 0 &&
                        contextMetrics!.composition.categories.some((c) => c.tokens > 0)
                          ? contextMetrics!.composition.categories.filter((c) => c.tokens > 0)
                          : [{ color: "hsl(var(--muted))" }]
                      ).map((cat, idx) => (
                        <Cell key={`cell-${idx}`} fill={cat.color || "#8884d8"} />
                      ))}
                    </Pie>
                    <RechartsTooltip
                      formatter={(v: any, name: any) => [`${fmtTokens(Number(v))} Tokens`, name]}
                    />
                  </PieChart>
                </div>

                <div className="flex-1 space-y-1.5">
                  {(
                    (contextMetrics?.composition.categories.length ?? 0) > 0 &&
                    contextMetrics!.composition.categories.some((c) => c.chars > 0)
                      ? contextMetrics!.composition.categories
                      : [
                          { name: "工具返回 (Tool Outputs)", percentage: 0, color: "#f59e0b" },
                          { name: "历史对话 (History)", percentage: 0, color: "#3b82f6" },
                          { name: "思考过程 (Thinking)", percentage: 0, color: "#8b5cf6" },
                          { name: "系统指令 (System Prompt)", percentage: 0, color: "#64748b" },
                        ]
                  ).map((cat) => (
                    <div key={cat.name} className="text-[10px]">
                      <div className="flex items-center justify-between">
                        <span className="flex items-center gap-1 truncate text-muted-foreground">
                          <span className="size-1.5 rounded-full" style={{ background: cat.color }} />
                          {cat.name.split(" ")[0]}
                        </span>
                        <span className="font-mono tabular-nums font-medium">
                          {cat.percentage ? `${cat.percentage.toFixed(1)}%` : "0%"}
                        </span>
                      </div>
                      <Progress value={cat.percentage ?? 0} className="h-1 mt-0.5" />
                    </div>
                  ))}
                </div>
              </div>
            </div>

            {/* 象限 2: 步骤耗时构成透视 */}
            <div className="flex flex-col rounded-lg border bg-muted/10 p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[11px] font-semibold flex items-center gap-1.5">
                  <ClockIcon className="size-3.5 text-muted-foreground" /> 各步骤耗时分布 (Latency)
                </span>
                <span className="text-[9px] text-muted-foreground">
                  总计 {fmtMs(contextMetrics?.summary.total_duration_ms ?? 0)}
                </span>
              </div>

              <div className="flex flex-col justify-around flex-1 space-y-2.5">
                {(() => {
                  const totalDur = (contextMetrics?.summary.total_duration_ms ?? 0) || 1;
                  const thinkMs = contextMetrics?.summary.thinking_duration_ms ?? 0;
                  const toolMs = contextMetrics?.summary.tool_duration_ms ?? 0;
                  const llmMs = contextMetrics?.summary.llm_duration_ms ?? 0;
                  const genMs = Math.max(0, llmMs - thinkMs);

                  const items = [
                    { label: "思考推理 (Thinking)", ms: thinkMs, color: "#8b5cf6", text: "text-violet-400" },
                    { label: "工具执行 (Tools)", ms: toolMs, color: "#f59e0b", text: "text-amber-400" },
                    { label: "模型生成 (Generation)", ms: genMs, color: "#06b6d4", text: "text-cyan-400" },
                  ];

                  return (
                    <>
                      <div className="flex h-3 w-full overflow-hidden rounded-full bg-muted">
                        {items.map((it) => (
                          <div
                            key={it.label}
                            className="h-full transition-all duration-300"
                            style={{
                              width: totalDur > 1 ? `${(it.ms / totalDur) * 100}%` : "0%",
                              background: it.color,
                            }}
                            title={`${it.label}: ${fmtMs(it.ms)}`}
                          />
                        ))}
                      </div>

                      <div className="space-y-2 mt-1">
                        {items.map((it) => (
                          <div key={it.label} className="flex items-center justify-between text-[10px]">
                            <span className="flex items-center gap-1 text-muted-foreground">
                              <span className="size-2 rounded-sm" style={{ background: it.color }} />
                              {it.label}
                            </span>
                            <div className="flex items-center gap-2">
                              <span className={cn("font-mono font-semibold tabular-nums", it.text)}>
                                {fmtMs(it.ms)}
                              </span>
                              <span className="text-[9px] text-muted-foreground w-8 text-right font-mono">
                                {totalDur > 1 ? `${Math.round((it.ms / totalDur) * 100)}%` : "0%"}
                              </span>
                            </div>
                          </div>
                        ))}
                      </div>
                    </>
                  );
                })()}
              </div>
            </div>

            {/* 象限 3: Token 每日消耗趋势 */}
            <div className="flex flex-col rounded-lg border bg-muted/10 p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[11px] font-semibold flex items-center gap-1.5">
                  <ZapIcon className="size-3.5 text-muted-foreground" /> Token 消耗趋势 (Daily Trend)
                </span>
                <div className="flex gap-2 text-[9px] text-muted-foreground">
                  {(["input", "output", "cacheRead"] as const).map((k) => (
                    <span key={k} className="flex items-center gap-1">
                      <span
                        className="inline-block size-1.5 rounded-sm"
                        style={{ background: dailyTrendConfig[k].color }}
                      />
                      {dailyTrendConfig[k].label}
                    </span>
                  ))}
                </div>
              </div>

              {dailyTokenData.length === 0 ? (
                <div className="flex flex-1 items-center justify-center rounded-lg border bg-background/30 text-[10px] text-muted-foreground min-h-[120px]">
                  暂无 Token 消耗趋势数据
                </div>
              ) : (
                <ChartContainer config={dailyTrendConfig} className="h-[120px] w-full">
                  <BarChart data={dailyTokenData} margin={{ top: 4, right: 4, bottom: 0, left: 0 }} maxBarSize={30}>
                    <CartesianGrid vertical={false} />
                    <XAxis
                      dataKey="date"
                      tickLine={false}
                      axisLine={false}
                      tick={{ fontSize: 9 }}
                      tickMargin={4}
                      interval="preserveStartEnd"
                    />
                    <YAxis
                      tickFormatter={(v) => fmtTokens(Number(v))}
                      tickLine={false}
                      axisLine={false}
                      tick={{ fontSize: 9 }}
                      width={38}
                    />
                    <ChartTooltip
                      cursor={false}
                      content={
                        <ChartTooltipContent
                          indicator="line"
                          formatter={(value, name) => (
                            <div className="flex w-full items-center justify-between gap-4 text-[10px]">
                              <span>
                                {(dailyTrendConfig as Record<string, { label: string }>)[String(name)]?.label ??
                                  String(name)}
                              </span>
                              <span className="font-mono font-semibold tabular-nums">{fmtTokens(Number(value))}</span>
                            </div>
                          )}
                        />
                      }
                    />
                    <Bar dataKey="input" stackId="1" fill="var(--color-input)" radius={[0, 0, 0, 0]} isAnimationActive={false} />
                    <Bar dataKey="output" stackId="1" fill="var(--color-output)" radius={[0, 0, 0, 0]} isAnimationActive={false} />
                    <Bar dataKey="cacheRead" stackId="1" fill="var(--color-cacheRead)" radius={[3, 3, 0, 0]} isAnimationActive={false} />
                  </BarChart>
                </ChartContainer>
              )}
            </div>

            {/* 象限 4: Top 5 最耗上下文工具排行榜 */}
            <div className="flex flex-col rounded-lg border bg-muted/10 p-3">
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[11px] font-semibold flex items-center gap-1.5">
                  <TerminalIcon className="size-3.5 text-muted-foreground" /> Top 5 最耗上下文工具
                </span>
                <span className="text-[9px] text-muted-foreground">上下文膨胀排行</span>
              </div>

              <div className="flex-1 space-y-1.5 overflow-hidden">
                {(contextMetrics?.composition.top_tool_outputs?.length ?? 0) === 0 ? (
                  <div className="flex h-full items-center justify-center text-[10px] text-muted-foreground py-4">
                    暂无工具执行与膨胀记录
                  </div>
                ) : (
                  contextMetrics!.composition.top_tool_outputs.slice(0, 5).map((tool, idx) => (
                    <div
                      key={idx}
                      className="flex items-center justify-between rounded border bg-background/50 px-2 py-1 text-[10px]"
                    >
                      <div className="flex items-center gap-1.5 truncate pr-2">
                        <span className="font-mono text-[9px] text-muted-foreground">#{idx + 1}</span>
                        <span className="font-semibold text-foreground truncate">{tool.tool || "tool"}</span>
                        <span className="text-[9px] text-muted-foreground truncate">{tool.summary}</span>
                      </div>
                      <div className="flex items-center gap-2 shrink-0">
                        <span className="font-mono text-amber-500">{fmtTokens(tool.estimated_tokens)} tok</span>
                        <span className="font-mono text-muted-foreground text-[9px]">{fmtMs(tool.duration_ms)}</span>
                      </div>
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>

          {/* Part 3: 底部综合透视栏 */}
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted/10 px-3 py-2 text-[10px]">
            <div className="flex items-center gap-2">
              <BotIcon className="size-3.5 text-muted-foreground" />
              <span className="font-medium text-foreground">Agent 角色分工:</span>
              {(contextMetrics?.fleet?.length ?? 0) === 0 ? (
                <span className="text-muted-foreground">Planner / Worker 准备就绪，运行任务时将自动实时统计</span>
              ) : (
                contextMetrics!.fleet.map((f) => (
                  <div key={f.worker} className="flex items-center gap-1 rounded bg-background/60 px-2 py-0.5 border">
                    <span className="font-medium">{f.role || f.worker}:</span>
                    <span className="font-mono text-blue-400">{fmtTokens(f.input_tokens + f.output_tokens)} tok</span>
                    <span className="text-muted-foreground">({f.calls}次)</span>
                  </div>
                ))
              )}
            </div>

            {tasks.length > 0 && (
              <Link
                href={`/function/tasks/detail?id=${tasks[0].id}&tab=context`}
                className="flex items-center gap-1 text-primary hover:underline font-medium text-[10px]"
              >
                查看最新任务完整上下文瀑布流 <ChevronRightIcon className="size-3" />
              </Link>
            )}
          </div>
        </div>
      </Card>

      {/* ── Row 3: 活动流 | 发现 ── */}
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {/* 活动流 */}
        <Card className="p-4">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-1.5 text-xs font-semibold">
              <ActivityIcon className="size-3.5 text-muted-foreground" />
              活动流
            </div>
            <div className="flex items-center gap-2">
              <span className="text-[10px] text-muted-foreground">
                {activity.filter((a) => a.kind !== "usage").length} 条事件
              </span>
              <Link
                href="/function/tasks"
                className="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground"
              >
                查看任务 <ArrowUpRightIcon className="size-3" />
              </Link>
            </div>
          </div>

          <div className="mb-2.5 flex flex-wrap gap-1.5">
            {(["tool_use", "tool_result", "text", "thinking", "result"] as const).map((kind) => {
              const count = activity.filter((a) => a.kind === kind).length;
              if (count === 0) return null;
              return (
                <span key={kind} className="rounded border bg-muted/20 px-1.5 py-0.5 text-[9px] text-muted-foreground">
                  {kind} <strong className="text-foreground/70">{count}</strong>
                </span>
              );
            })}
          </div>

          <div className="divide-y">
            {recentActivity.length === 0 ? (
              <div className="py-4 text-center text-xs text-muted-foreground">暂无活动记录</div>
            ) : (
              recentActivity.map((a) => (
                <div key={a.seq} className="flex gap-2.5 py-2">
                  <span
                    className={cn(
                      "shrink-0 self-start rounded border px-1.5 py-0.5 font-mono text-[9px]",
                      workerBg(a.worker),
                    )}
                  >
                    {a.worker}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="text-[11px] font-medium">{kindLabel(a)}</div>
                    <div className="mt-0.5 truncate text-[10px] text-muted-foreground">{a.summary}</div>
                  </div>
                  <div className="shrink-0 text-right">
                    <Badge variant="outline" className="px-1.5 py-0 text-[9px] text-muted-foreground">
                      {a.kind}
                    </Badge>
                    <div className="mt-0.5 text-[9px] tabular-nums text-muted-foreground">{fmtRel(a.ts)}</div>
                  </div>
                </div>
              ))
            )}
          </div>
        </Card>

        {/* 发现 */}
        <Card className="p-4">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-1.5 text-xs font-semibold">
              <BugIcon className="size-3.5 text-muted-foreground" />
              发现
            </div>
            <Link
              href="/function/findings"
              className="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground"
            >
              全部 <ArrowUpRightIcon className="size-3" />
            </Link>
          </div>

          <div className="divide-y">
            {recentFindings.length === 0 ? (
              <div className="py-4 text-center text-xs text-muted-foreground">暂无发现</div>
            ) : (
              recentFindings.map((f) => (
                <div key={f.id} className="flex items-start gap-2 py-2">
                  <StatusBadge
                    domain="severity"
                    value={f.severity}
                    className="mt-0.5 shrink-0 px-1.5 py-0 text-[10px]"
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-[11px] font-semibold">{f.vulnclass}</span>
                      {f.task_description && (
                        <span className="truncate text-[10px] text-muted-foreground">{f.task_description}</span>
                      )}
                    </div>
                    <div className="mt-0.5 line-clamp-2 text-[10px] leading-snug text-muted-foreground">
                      {f.summary}
                    </div>
                  </div>
                  <span className="shrink-0 text-[9px] tabular-nums text-muted-foreground">{fmtRel(f.ts)}</span>
                </div>
              ))
            )}
          </div>
        </Card>
      </div>

      {/* ── Row 4: 任务表格 ── */}
      <Card className="overflow-hidden p-0">
        <div className="flex items-center justify-between border-b px-4 py-3">
          <div className="flex items-center gap-1.5 text-xs font-semibold">
            <ClockIcon className="size-3.5 text-muted-foreground" />
            任务
          </div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] text-muted-foreground">{tasks.length} 个任务</span>
            <Link
              href="/function/tasks"
              className="flex items-center gap-0.5 text-[10px] text-muted-foreground hover:text-foreground"
            >
              全部 <ArrowUpRightIcon className="size-3" />
            </Link>
          </div>
        </div>
        <table className="w-full border-collapse text-xs">
          <thead>
            <tr className="border-b">
              {["任务", "状态", "引擎", "目标进度", "在途", "最近活动"].map((h) => (
                <th
                  key={h}
                  className="px-4 py-2 text-left text-[9px] font-semibold uppercase tracking-widest text-muted-foreground first:pl-4"
                >
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {sortedTasks.length === 0 ? (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-xs text-muted-foreground">
                  暂无任务
                </td>
              </tr>
            ) : (
              sortedTasks.map((t) => {
                const goalsPct = t.goals_total ? Math.round(((t.goals_met ?? 0) / t.goals_total) * 100) : null;
                return (
                  <tr key={t.id} className="border-b last:border-0 transition-colors hover:bg-muted/30">
                    <td className="max-w-xs px-4 py-3">
                      <Link href={`/function/tasks/detail?id=${t.id}`} className="group flex flex-col">
                        <span className="truncate font-medium group-hover:underline">{t.description}</span>
                        <span className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">{t.id}</span>
                      </Link>
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge domain="task" value={t.status} dot className="px-1.5 py-0 text-[10px]" />
                    </td>
                    <td className="px-4 py-3">
                      <StatusBadge
                        domain="engine"
                        value={t.engine_mode ?? "idle"}
                        className="px-1.5 py-0 text-[10px]"
                      />
                    </td>
                    <td className="px-4 py-3">
                      {goalsPct !== null ? (
                        <div className="flex items-center gap-2">
                          <Progress value={goalsPct} className="h-1 w-14" />
                          <span className="tabular-nums text-muted-foreground">
                            {t.goals_met ?? 0}/{t.goals_total}
                          </span>
                        </div>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 tabular-nums">
                      {(t.in_flight ?? 0) > 0 ? (
                        <span className="font-semibold">{t.in_flight}</span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 tabular-nums text-muted-foreground">
                      {fmtRel(t.last_activity_unix ?? t.created_unix)}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </Card>

      {/* ── Row 5: 资产分布 | 拦截 & 待审批 ── */}
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {/* 资产分布 */}
        <Card className="p-4">
          <SectionTitle icon={NetworkIcon} sub="按类型">
            资产分布
          </SectionTitle>

          {assetByType.length === 0 ? (
            <div className="py-6 text-center text-xs text-muted-foreground">暂无资产数据</div>
          ) : (
            <div className="flex flex-col gap-2">
              {assetByType.map(([type, count]) => (
                <div key={type} className="flex items-center gap-2 text-xs">
                  <span className="w-8 shrink-0 text-right text-[10px] text-muted-foreground">
                    {ASSET_TYPE_LABELS[type] ?? type}
                  </span>
                  <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                    <div
                      className={cn("h-full rounded-full", ASSET_COLORS[type] ?? "bg-slate-400")}
                      style={{ width: `${(count / assetMax) * 100}%` }}
                    />
                  </div>
                  <span className="w-4 text-right tabular-nums">{count}</span>
                </div>
              ))}
            </div>
          )}

          <div className="mt-3 border-t pt-3 text-[10px] text-muted-foreground">共 {totalAssets} 节点</div>
        </Card>

        {/* 系统状态 & 待审批 */}
        <Card className="p-4">
          <SectionTitle icon={ShieldCheckIcon}>系统状态</SectionTitle>

          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2">
              <div className="text-[10px] text-muted-foreground">LLM 配置</div>
              <Badge
                variant="outline"
                className={cn(
                  "text-[10px]",
                  stats?.llm_configured
                    ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-400"
                    : "border-red-500/30 bg-red-500/10 text-red-400",
                )}
              >
                {stats?.llm_configured ? "已配置" : "未配置"}
              </Badge>
            </div>

            {activeProfile && (
              <div className="flex items-center justify-between rounded-lg border bg-muted/20 px-3 py-2">
                <div className="text-[10px] text-muted-foreground">激活模型</div>
                <span className="font-mono text-[10px]">{activeProfile.model}</span>
              </div>
            )}
          </div>

          {/* pending approvals */}
          {pendingCount > 0 && (
            <div className="mt-3">
              <div className="mb-1.5 text-[10px] font-medium text-amber-400">待审批 ({pendingCount})</div>
              <div className="flex flex-col gap-1.5">
                {pending.slice(0, 3).map((p) => (
                  <Link
                    key={p.id}
                    href="/system/intercept/approvals"
                    className="flex items-center justify-between rounded-lg border border-amber-500/20 bg-amber-500/5 px-2.5 py-2 hover:bg-amber-500/10"
                  >
                    <div className="min-w-0">
                      <div className="text-[10px] font-medium">{p.tool_name}</div>
                      <div className="text-[9px] text-muted-foreground">{p.agent_name}</div>
                    </div>
                    <ArrowUpRightIcon className="size-3 shrink-0 text-amber-400" />
                  </Link>
                ))}
                {pendingCount > 3 && (
                  <Link
                    href="/system/intercept/approvals"
                    className="text-center text-[10px] text-muted-foreground hover:text-foreground"
                  >
                    还有 {pendingCount - 3} 条…
                  </Link>
                )}
              </div>
            </div>
          )}

          {pendingCount === 0 && (
            <div className="mt-3 rounded-lg border bg-muted/10 px-3 py-3 text-center text-[10px] text-muted-foreground">
              <ShieldCheckIcon className="mx-auto mb-1 size-4 text-emerald-500/50" />
              无待审批拦截
            </div>
          )}
        </Card>
      </div>
    </div>
  );
}
