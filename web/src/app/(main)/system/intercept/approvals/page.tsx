import { SecurityHubNav } from "@/components/security-hub-nav";
import { ApprovalRecords } from "@/components/approval-records";

export default function ApprovalsPage() {
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-5 p-4 sm:p-6">
      <SecurityHubNav />
      <ApprovalRecords hideHeader />
    </div>
  );
}
