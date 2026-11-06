"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Bot, Brain, Plug, Sparkles, Wrench } from "lucide-react";

import { cn } from "@/lib/utils";

const AGENT_HUB_TABS = [
  { id: "agents", label: "角色编排", href: "/system/agents", icon: Bot },
  { id: "llm", label: "模型接入 (LLM)", href: "/system/llm", icon: Brain },
  { id: "mcp", label: "MCP 扩展", href: "/system/mcp", icon: Plug },
  { id: "skills", label: "技能库 (Skill)", href: "/system/skills", icon: Sparkles },
  { id: "tools", label: "工具管理 (Tools)", href: "/system/tools", icon: Wrench },
] as const;

export function AgentHubNav({
  title = "智能体能力中枢",
  description = "角色编排 · 大脑模型接入 · MCP 协议 · 技能包 · 底层工具库",
  action,
}: {
  title?: string;
  description?: string;
  action?: React.ReactNode;
}) {
  const pathname = usePathname();
  const cleanPath = (pathname ? pathname.replace(/\/+$/, "") : "") || "/";

  return (
    <div className="flex flex-col gap-3 pb-1 border-b">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
          <p className="text-muted-foreground text-xs">{description}</p>
        </div>
        {action && <div className="flex items-center gap-2">{action}</div>}
      </div>

      <nav className="flex gap-1 overflow-x-auto no-scrollbar pt-1">
        {AGENT_HUB_TABS.map((tab) => {
          const Icon = tab.icon;
          const cleanHref = tab.href.replace(/\/+$/, "") || "/";
          const isActive =
            cleanHref === "/system/agents"
              ? cleanPath === "/system/agents" || cleanPath.startsWith("/system/agents/")
              : cleanPath === cleanHref;

          return (
            <Link
              key={tab.id}
              href={tab.href}
              className={cn(
                "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors whitespace-nowrap",
                isActive
                  ? "bg-primary text-primary-foreground shadow-xs"
                  : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
              )}
            >
              <Icon className="size-3.5" />
              <span>{tab.label}</span>
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
