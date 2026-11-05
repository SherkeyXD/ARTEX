"use client";

import * as React from "react";

import { useSearchParams } from "next/navigation";

import { AgentEditor } from "@/components/agent-editor";
import { BackButton } from "@/components/back-button";

// Deep-link editor page for one agent. The primary flow is the drawer on
// /system/agents; this page reuses the same AgentEditor component full-width so a
// direct URL (or an external link) still opens the editor.
function AgentDetailInner() {
  const searchParams = useSearchParams();
  const key = searchParams.get("key") ?? "";

  return (
    <div className="flex flex-1 flex-col gap-4">
      <div className="flex items-center gap-2">
        <BackButton fallback="/system/agents" size="icon-sm" title="返回 Agent 列表" />
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Agent</h1>
          <p className="text-muted-foreground font-mono text-xs">{key}</p>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col rounded-lg border">
        <AgentEditor agentKey={key} />
      </div>
    </div>
  );
}

// useSearchParams must sit under a Suspense boundary for static export.
export default function AgentDetailPage() {
  return (
    <React.Suspense fallback={null}>
      <AgentDetailInner />
    </React.Suspense>
  );
}
