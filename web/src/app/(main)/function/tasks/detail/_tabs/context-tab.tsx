"use client";

import * as React from "react";
import {
  ActivityIcon,
  BotIcon,
  BrainIcon,
  CheckCircle2Icon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  CoinsIcon,
  CopyIcon,
  CpuIcon,
  GaugeIcon,
  LayersIcon,
  RefreshCwIcon,
  TerminalIcon,
  WrenchIcon,
  ZapIcon,
} from "lucide-react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from "recharts";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useVisibilityPoll } from "@/hooks/use-visibility-poll";
import { api } from "@/lib/api";
import type {
  ContextCategory,
  EvolutionTurn,
  FleetAgent,
  TaskContextMetricsReport,
  TopToolOutput,
  WaterfallStep,
} from "@/lib/types";
import { cn } from "@/lib/utils";

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
  if (n <= 0) return "0";
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`;
  return n.toLocaleString();
}

function fmtChars(chars: number): string {
  if (chars <= 0) return "0 B";
  if (chars >= 1024 * 1024) return `${(chars / (1024 * 1024)).toFixed(1)} MB`;
  if (chars >= 1024) return `${(chars / 1024).toFixed(1)} KB`;
  return `${chars} B`;
}

export function ContextTab({ taskId }: { taskId: string }) {
  const [report, setReport] = React.useState<TaskContextMetricsReport | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [expandedWaterfallId, setExpandedWaterfallId] = React.useState<number | null>(null);
  const [waterfallFilter, setWaterfallFilter] = React.useState<string>("all");
  const reportSigRef = React.useRef("");

  const load = React.useCallback(async () => {
    try {
      const data = await api.taskContextMetrics(taskId);
      const sig = JSON.stringify([
        data.summary.total_tokens,
        data.summary.step_count,
        data.summary.total_duration_ms,
        data.turns.length,
        data.fleet.length,
        data.composition.top_tool_outputs.length,
      ]);
      if (sig !== reportSigRef.current) {
        reportSigRef.current = sig;
        setReport(data);
      }
    } catch {
      // transient failures keep stale snapshot
    } finally {
      setLoading(false);
    }
  }, [taskId]);

  useVisibilityPoll(load, 5000);

  const summary = report?.summary;
  const composition = report?.composition;
  const turns = report?.turns ?? [];
  const waterfall = report?.waterfall ?? [];
  const fleet = report?.fleet ?? [];

  const filteredWaterfall = React.useMemo(() => {
    if (waterfallFilter === "all") return waterfall;
    return waterfall.filter((s) => s.kind === waterfallFilter);
  }, [waterfall, waterfallFilter]);

  if (loading && !report) {
    return (
      <div className="flex flex-col gap-4 p-4">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24 w-full rounded-xl" />
          ))}
        </div>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Skeleton className="h-72 w-full rounded-xl" />
          <Skeleton className="h-72 w-full rounded-xl" />
        </div>
      </div>
    );
  }

  const hasData = (summary?.total_tokens ?? 0) > 0 || (summary?.step_count ?? 0) > 0;

  return (
    <div className="flex flex-col gap-5 p-4">
      {/* ── 1. Top KPI Summary Ribbon ─────────────────────────────────────── */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Card className="border-border/60 shadow-xs">
          <CardHeader className="flex flex-row items-center justify-between pb-1 pt-3 px-4">
            <span className="text-xs font-medium text-muted-foreground">总消耗 Token</span>
            <CoinsIcon className="size-4 text-amber-500" />
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-bold tracking-tight font-mono">{fmtTokens(summary?.total_tokens ?? 0)}</span>
              <span className="text-xs text-muted-foreground font-mono">tokens</span>
            </div>
            <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
              <span>入 {fmtTokens(summary?.input_tokens ?? 0)}</span>
              <span>•</span>
              <span>出 {fmtTokens(summary?.output_tokens ?? 0)}</span>
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-xs">
          <CardHeader className="flex flex-row items-center justify-between pb-1 pt-3 px-4">
            <span className="text-xs font-medium text-muted-foreground">Prompt 缓存命中</span>
            <ZapIcon className="size-4 text-emerald-500" />
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-bold tracking-tight font-mono text-emerald-600 dark:text-emerald-400">
                {((summary?.cache_hit_rate ?? 0) * 100).toFixed(1)}%
              </span>
              <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 border-emerald-500/30 text-emerald-600 dark:text-emerald-400">
                {summary && summary.cache_hit_rate > 0.4 ? "高命中" : "待优化"}
              </Badge>
            </div>
            <div className="mt-1 flex items-center gap-1.5 text-[11px] text-muted-foreground font-mono">
              <span>读 {fmtTokens(summary?.cache_read_tokens ?? 0)}</span>
              <span>/</span>
              <span>写 {fmtTokens(summary?.cache_write_tokens ?? 0)}</span>
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-xs">
          <CardHeader className="flex flex-row items-center justify-between pb-1 pt-3 px-4">
            <span className="text-xs font-medium text-muted-foreground">总执行耗时</span>
            <ClockIcon className="size-4 text-blue-500" />
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-bold tracking-tight font-mono">{fmtMs(summary?.total_duration_ms ?? 0)}</span>
            </div>
            <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
              <span>思考 {fmtMs(summary?.thinking_duration_ms ?? 0)}</span>
              <span>•</span>
              <span>工具 {fmtMs(summary?.tool_duration_ms ?? 0)}</span>
            </div>
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-xs">
          <CardHeader className="flex flex-row items-center justify-between pb-1 pt-3 px-4">
            <span className="text-xs font-medium text-muted-foreground">生成速率 & 步数</span>
            <GaugeIcon className="size-4 text-indigo-500" />
          </CardHeader>
          <CardContent className="px-4 pb-3">
            <div className="flex items-baseline gap-1.5">
              <span className="text-xl font-bold tracking-tight font-mono">
                {(summary?.avg_tokens_per_sec ?? 0) > 0 ? summary!.avg_tokens_per_sec.toFixed(1) : "—"}
              </span>
              <span className="text-xs text-muted-foreground font-mono">t/s</span>
            </div>
            <div className="mt-1 flex items-center gap-2 text-[11px] text-muted-foreground">
              <span>{summary?.step_count ?? 0} 个步骤</span>
              <span>•</span>
              <span>{summary?.tool_calls ?? 0} 次工具调用</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* ── 2. Context Composition & Heavy Tool Consumers ──────────────────── */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        {/* Left: Donut Composition Chart */}
        <Card className="border-border/60 shadow-xs lg:col-span-6 flex flex-col">
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="text-base flex items-center gap-2">
                  <LayersIcon className="size-4 text-indigo-500" />
                  上下文构成拆解 (Context Breakdown)
                </CardTitle>
                <CardDescription className="text-xs">
                  分析当前会话或历史执行中，各类内容占用的 Context 比例
                </CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="flex-1 flex flex-col justify-between pt-2">
            {composition && composition.categories.length > 0 ? (
              <div className="flex flex-col gap-4">
                <div className="h-56 w-full flex items-center justify-center">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={composition.categories}
                        dataKey="tokens"
                        nameKey="name"
                        cx="50%"
                        cy="50%"
                        innerRadius={55}
                        outerRadius={85}
                        paddingAngle={3}
                      >
                        {composition.categories.map((entry, index) => (
                          <Cell key={`cell-${index}`} fill={entry.color} />
                        ))}
                      </Pie>
                      <RechartsTooltip
                        formatter={(val: unknown, name: unknown) => [
                          `${Number(val ?? 0).toLocaleString()} tokens`,
                          String(name ?? ""),
                        ]}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                </div>

                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {composition.categories.map((cat) => (
                    <div
                      key={cat.category}
                      className="flex items-center justify-between rounded-lg border bg-muted/20 p-2.5 text-xs"
                    >
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="size-2.5 shrink-0 rounded-full" style={{ backgroundColor: cat.color }} />
                        <span className="font-medium truncate">{cat.name}</span>
                      </div>
                      <div className="flex items-baseline gap-1.5 shrink-0 font-mono">
                        <span className="font-semibold">{cat.percentage.toFixed(1)}%</span>
                        <span className="text-[10px] text-muted-foreground">({fmtTokens(cat.tokens)})</span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <div className="flex h-56 items-center justify-center text-xs text-muted-foreground">
                暂无足够的上下文构成数据
              </div>
            )}
          </CardContent>
        </Card>

        {/* Right: Top Context Consumers (Heaviest Tool Outputs) */}
        <Card className="border-border/60 shadow-xs lg:col-span-6 flex flex-col">
          <CardHeader className="pb-2">
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className="text-base flex items-center gap-2">
                  <WrenchIcon className="size-4 text-amber-500" />
                  Context 消耗大户 (Top Tool Outputs)
                </CardTitle>
                <CardDescription className="text-xs">
                  工具返回的大体积输出通常占上下文 70% 以上，是导致 Token 膨胀的根源
                </CardDescription>
              </div>
            </div>
          </CardHeader>
          <CardContent className="flex-1 pt-2">
            {composition && composition.top_tool_outputs.length > 0 ? (
              <ScrollArea className="h-72">
                <div className="flex flex-col gap-2.5 pr-3">
                  {composition.top_tool_outputs.map((item, idx) => (
                    <div
                      key={idx}
                      className="flex flex-col gap-1.5 rounded-lg border bg-muted/15 p-2.5 text-xs transition-colors hover:bg-muted/30"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <div className="flex items-center gap-2 min-w-0">
                          <Badge variant="secondary" className="font-mono text-[10px] shrink-0">
                            #{idx + 1} {item.tool}
                          </Badge>
                          <span className="font-mono truncate font-medium text-foreground/90" title={item.summary}>
                            {item.summary || "（无指令摘要）"}
                          </span>
                        </div>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
                          onClick={() => {
                            navigator.clipboard.writeText(item.summary);
                            toast.success("已复制工具指令");
                          }}
                          title="复制指令"
                        >
                          <CopyIcon className="size-3" />
                        </Button>
                      </div>

                      <div className="flex items-center justify-between text-[11px] text-muted-foreground font-mono">
                        <div className="flex items-center gap-3">
                          <span className="text-amber-600 dark:text-amber-400 font-semibold">
                            {fmtChars(item.char_count)}
                          </span>
                          <span>≈ {fmtTokens(item.estimated_tokens)} tok</span>
                        </div>
                        <span>耗时 {fmtMs(item.duration_ms)}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </ScrollArea>
            ) : (
              <div className="flex h-56 items-center justify-center text-xs text-muted-foreground">
                当前任务尚未产生工具调用输出
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* ── 3. Step Latency & Token Waterfall ──────────────────────────────── */}
      <Card className="border-border/60 shadow-xs">
        <CardHeader className="pb-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <CardTitle className="text-base flex items-center gap-2">
                <ActivityIcon className="size-4 text-emerald-500" />
                执行耗时甘特瀑布流 (Step Latency Waterfall)
              </CardTitle>
              <CardDescription className="text-xs">
                按执行时间线直观展示每个步骤（思考、工具执行、回复生成）的实际耗时
              </CardDescription>
            </div>

            <div className="flex items-center gap-1.5">
              {[
                { id: "all", label: "全部" },
                { id: "thinking", label: "思考" },
                { id: "tool_result", label: "工具" },
                { id: "text", label: "回复" },
              ].map((f) => (
                <Button
                  key={f.id}
                  variant={waterfallFilter === f.id ? "secondary" : "ghost"}
                  size="sm"
                  className="h-7 text-xs px-2.5"
                  onClick={() => setWaterfallFilter(f.id)}
                >
                  {f.label}
                </Button>
              ))}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {filteredWaterfall.length > 0 ? (
            <ScrollArea className="h-80">
              <div className="flex flex-col gap-2 pr-3">
                {filteredWaterfall.map((step) => {
                  const isExpanded = expandedWaterfallId === step.id;
                  const kindColor =
                    step.kind === "tool_result"
                      ? "bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30"
                      : step.kind === "thinking"
                      ? "bg-purple-500/15 text-purple-700 dark:text-purple-300 border-purple-500/30"
                      : "bg-blue-500/15 text-blue-700 dark:text-blue-300 border-blue-500/30";

                  const barBg =
                    step.kind === "tool_result"
                      ? "bg-amber-500"
                      : step.kind === "thinking"
                      ? "bg-purple-500"
                      : "bg-blue-500";

                  const maxStepMs = Math.max(1000, ...filteredWaterfall.map((s) => s.duration_ms));
                  const barWidthPercent = Math.min(100, Math.max(4, (step.duration_ms / maxStepMs) * 100));

                  return (
                    <div
                      key={step.id}
                      className="group flex flex-col rounded-lg border bg-card p-2 text-xs transition-colors hover:border-primary/40"
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-2 min-w-0 flex-1">
                          <button
                            type="button"
                            onClick={() => setExpandedWaterfallId(isExpanded ? null : step.id)}
                            className="text-muted-foreground hover:text-foreground"
                          >
                            {isExpanded ? <ChevronDownIcon className="size-3.5" /> : <ChevronRightIcon className="size-3.5" />}
                          </button>

                          <Badge variant="outline" className={cn("text-[10px] px-1.5 py-0 h-4.5 font-mono", kindColor)}>
                            {step.kind === "tool_result"
                              ? `Tool: ${step.tool}`
                              : step.kind === "thinking"
                              ? "Thinking"
                              : "Answer"}
                          </Badge>

                          <span className="font-mono text-muted-foreground text-[11px] shrink-0">
                            {step.worker}
                          </span>

                          <span className="truncate text-foreground/80 font-mono text-[11px]" title={step.summary}>
                            {step.summary}
                          </span>
                        </div>

                        {/* Latency Bar & Value */}
                        <div className="flex items-center gap-3 shrink-0">
                          <div className="hidden sm:flex w-32 h-2.5 rounded-full bg-muted/40 overflow-hidden">
                            <div className={cn("h-full rounded-full transition-all", barBg)} style={{ width: `${barWidthPercent}%` }} />
                          </div>
                          <span className="w-16 text-right font-mono font-medium text-foreground">
                            {fmtMs(step.duration_ms)}
                          </span>
                        </div>
                      </div>

                      {isExpanded && (
                        <div className="mt-2.5 border-t pt-2 text-muted-foreground text-[11px] flex flex-col gap-1">
                          <div className="flex items-center gap-4">
                            <span>时间点：{new Date(step.timestamp).toLocaleTimeString("zh-CN")}</span>
                            <span>相对起点：+{fmtMs(step.offset_ms)}</span>
                            {step.char_count ? <span>体量：{fmtChars(step.char_count)}</span> : null}
                          </div>
                          <div className="rounded bg-muted/40 p-2 font-mono text-[11px] whitespace-pre-wrap max-h-32 overflow-auto text-foreground/90">
                            {step.summary}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </ScrollArea>
          ) : (
            <div className="flex h-40 items-center justify-center text-xs text-muted-foreground">
              暂无符合筛选条件的步骤记录
            </div>
          )}
        </CardContent>
      </Card>

      {/* ── 4. Turn Evolution Chart & Multi-Agent Fleet ────────────────────── */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        {/* Left: Evolution Turns Chart */}
        <Card className="border-border/60 shadow-xs lg:col-span-6 flex flex-col">
          <CardHeader className="pb-2">
            <CardTitle className="text-base flex items-center gap-2">
              <CpuIcon className="size-4 text-purple-500" />
              轮次演进趋势 (Turn Evolution)
            </CardTitle>
            <CardDescription className="text-xs">
              追踪各轮对话中输入 Token 与缓存增长情况
            </CardDescription>
          </CardHeader>
          <CardContent className="flex-1 pt-2">
            {turns.length > 0 ? (
              <div className="h-64 w-full">
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={turns} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" opacity={0.2} />
                    <XAxis dataKey="turn_index" tickLine={false} tick={{ fontSize: 10 }} />
                    <YAxis tickLine={false} tick={{ fontSize: 10 }} tickFormatter={fmtTokens} />
                    <RechartsTooltip
                      formatter={(val: unknown, name: unknown) => [
                        `${Number(val ?? 0).toLocaleString()} tokens`,
                        name === "cumulative_input" ? "累积输入" : "缓存命中",
                      ]}
                      labelFormatter={(label) => `第 ${label} 轮模型交互`}
                    />
                    <Area
                      type="monotone"
                      dataKey="cumulative_input"
                      stroke="#8b5cf6"
                      fill="#8b5cf6"
                      fillOpacity={0.2}
                      name="cumulative_input"
                    />
                    <Area
                      type="monotone"
                      dataKey="cumulative_cache"
                      stroke="#10b981"
                      fill="#10b981"
                      fillOpacity={0.2}
                      name="cumulative_cache"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            ) : (
              <div className="flex h-64 items-center justify-center text-xs text-muted-foreground">
                当前任务尚未记录到模型交互轮次
              </div>
            )}
          </CardContent>
        </Card>

        {/* Right: Multi-Agent Fleet View */}
        <Card className="border-border/60 shadow-xs lg:col-span-6 flex flex-col">
          <CardHeader className="pb-2">
            <CardTitle className="text-base flex items-center gap-2">
              <BotIcon className="size-4 text-blue-500" />
              多 Agent 舰队视图 (Fleet View)
            </CardTitle>
            <CardDescription className="text-xs">
              监控规划者（Planner）与各意图执行者（Worker）的算力分配
            </CardDescription>
          </CardHeader>
          <CardContent className="flex-1 pt-2">
            {fleet.length > 0 ? (
              <div className="flex flex-col gap-2.5">
                {fleet.map((agent, i) => (
                  <div
                    key={i}
                    className="flex flex-col gap-2 rounded-lg border bg-muted/15 p-3 text-xs"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold">{agent.role}</span>
                        <Badge variant="outline" className="font-mono text-[10px] px-1 py-0 h-4">
                          {agent.model || "default"}
                        </Badge>
                      </div>
                      <Badge
                        variant="outline"
                        className="text-[10px] border-emerald-500/30 text-emerald-600 dark:text-emerald-400"
                      >
                        已完成
                      </Badge>
                    </div>

                    <div className="grid grid-cols-3 gap-2 text-muted-foreground text-[11px] font-mono pt-1 border-t border-border/40">
                      <div>
                        <span className="block text-[10px] text-muted-foreground/70">消耗 Token</span>
                        <span className="font-semibold text-foreground">
                          {fmtTokens(agent.input_tokens + agent.output_tokens)}
                        </span>
                      </div>
                      <div>
                        <span className="block text-[10px] text-muted-foreground/70">调用耗时</span>
                        <span className="font-semibold text-foreground">{fmtMs(agent.duration_ms)}</span>
                      </div>
                      <div>
                        <span className="block text-[10px] text-muted-foreground/70">步骤 / 工具</span>
                        <span className="font-semibold text-foreground">
                          {agent.step_count} / {agent.tool_calls}
                        </span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="flex h-64 items-center justify-center text-xs text-muted-foreground">
                暂无 Agent 舰队执行数据
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
