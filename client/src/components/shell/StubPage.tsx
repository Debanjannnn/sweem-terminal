import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";

export function StubPage({ title, message }: { title: string; message: string }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-4">
      <Card className="p-4">
        <h1 className="text-sm font-medium">{title}</h1>
        <EmptyState message={message} />
      </Card>
    </div>
  );
}
