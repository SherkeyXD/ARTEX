"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Ban, ClipboardList, ShieldAlert } from "lucide-react";

import { useVisibilityPoll } from "@/hooks/use-visibility-poll";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";

const SECURITY_HUB_TABS = [
  { id: "intercept", label: "动作拦截规则", href: "/system/intercept", icon: ShieldAlert },
  { id: "asset-intercept", label: "资产边界拦截", href: "/system/intercept/assets", icon: Ban },
  { id: "approvals", label: "审批与审计记录", href: "/system/intercept/approvals", icon: ClipboardList },
] as const;

export function SecurityHubNav({
  title = "安全风控中心",
  description = "高危行为拦截 · 资产出网边界防护 · 人机协同审批审计",
  action,
}: {
  title?: string;
  description?: string;
  action?: React.ReactNode;
}) {
  const pathname = usePathname();
  const [pendingCount, setPendingCount] = React.useState(0);

  useVisibilityPoll(async () => {
    try {
      const list = await api.interceptPending();
      setPendingCount(list.length);
    } catch {
      /* ignore */
    }
  }, 5000);

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
        {SECURITY_HUB_TABS.map((tab) => {
          const Icon = tab.icon;
          const isActive =
            tab.href === "/system/intercept"
              ? pathname === "/system/intercept"
              : pathname.startsWith(tab.href);

          return (
            <Link
              key={tab.id}
              href={tab.href}
              className={cn(
                "relative flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors whitespace-nowrap",
                isActive
                  ? "bg-primary text-primary-foreground shadow-xs"
                  : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
              )}
            >
              <Icon className="size-3.5" />
              <span>{tab.label}</span>
              {tab.id === "approvals" && pendingCount > 0 && (
                <span
                  className={cn(
                    "ml-1 inline-flex h-4 min-w-[16px] items-center justify-center rounded-full px-1 text-[10px] font-semibold",
                    isActive ? "bg-amber-400 text-amber-950" : "bg-amber-500 text-white",
                  )}
                >
                  {pendingCount > 99 ? "99+" : pendingCount}
                </span>
              )}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
