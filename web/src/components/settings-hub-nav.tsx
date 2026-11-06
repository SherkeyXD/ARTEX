"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { BellRing, ScrollText, Settings2 } from "lucide-react";

import { cn } from "@/lib/utils";

const SETTINGS_HUB_TABS = [
  { id: "settings", label: "全局配置", href: "/system/settings", icon: Settings2 },
  { id: "notify", label: "通知推送", href: "/system/notify", icon: BellRing },
  { id: "logs", label: "系统日志", href: "/system/logs", icon: ScrollText },
] as const;

export function SettingsHubNav({
  title = "系统运维配置",
  description = "运行时与网络参数 · 漏洞消息推送 · 实时系统日志",
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
        {SETTINGS_HUB_TABS.map((tab) => {
          const Icon = tab.icon;
          const cleanHref = tab.href.replace(/\/+$/, "") || "/";
          const isActive = cleanPath === cleanHref;

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
