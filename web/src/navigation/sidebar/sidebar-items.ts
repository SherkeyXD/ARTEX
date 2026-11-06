import {
  Activity,
  Ban,
  BellRing,
  Bot,
  Brain,
  Bug,
  ClipboardList,
  FolderOpen,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Network,
  Plug,
  ScrollText,
  Settings2,
  ShieldAlert,
  Sparkles,
  Target,
  Terminal,
  Wrench,
} from "lucide-react";

export type NavBadge = "new" | "soon";

export interface NavSubItem {
  id: string;
  title: string;
  url: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

interface NavItemBase {
  id: string;
  title: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

export interface NavMainLinkItem extends NavItemBase {
  url: string;
  subItems?: never;
}

export interface NavMainParentItem extends NavItemBase {
  subItems: NavSubItem[];
}

export type NavMainItem = NavMainLinkItem | NavMainParentItem;

export interface NavGroup {
  id: number;
  label?: string;
  items: NavMainItem[];
}

export const sidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "功能",
    items: [
      { id: "dashboard", title: "仪表盘", url: "/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "对话", url: "/chat", icon: MessageSquare },
      { id: "tasks", title: "任务", url: "/function/tasks", icon: Target },
      { id: "findings", title: "发现", url: "/function/findings", icon: Bug },
      { id: "commands", title: "工具执行", url: "/function/commands", icon: Terminal },
      { id: "assets", title: "资产", url: "/function/assets", icon: Network },
      { id: "workspace", title: "工作空间", url: "/function/workspace", icon: FolderOpen },
    ],
  },
  {
    id: 2,
    label: "系统",
    items: [
      {
        id: "agent-hub",
        title: "智能体中枢",
        icon: Bot,
        subItems: [
          { id: "agents", title: "角色编排", url: "/system/agents", icon: Bot },
          { id: "llm", title: "模型接入", url: "/system/llm", icon: Brain },
          { id: "mcp", title: "MCP 扩展", url: "/system/mcp", icon: Plug },
          { id: "skills", title: "技能库", url: "/system/skills", icon: Sparkles },
          { id: "tools", title: "工具管理", url: "/system/tools", icon: Wrench },
        ],
      },
      {
        id: "security-hub",
        title: "安全风控",
        icon: ShieldAlert,
        subItems: [
          { id: "intercept", title: "动作拦截", url: "/system/intercept", icon: ShieldAlert },
          { id: "asset-intercept", title: "资产边界", url: "/system/intercept/assets", icon: Ban },
          { id: "approvals", title: "审批审计", url: "/system/intercept/approvals", icon: ClipboardList },
        ],
      },
      {
        id: "settings-hub",
        title: "系统运维",
        icon: Settings2,
        subItems: [
          { id: "settings", title: "全局配置", url: "/system/settings", icon: Settings2 },
          { id: "notify", title: "通知推送", url: "/system/notify", icon: BellRing },
          { id: "logs", title: "系统日志", url: "/system/logs", icon: ScrollText },
        ],
      },
    ],
  },
];
