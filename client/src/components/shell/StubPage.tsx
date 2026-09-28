import { Card } from "@/components/ui/Card";
import { EmptyState } from "@/components/ui/EmptyState";
import { SectionTitle } from "@/components/ui/SectionTitle";

export function StubPage({ title, message }: { title: string; message: string }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-4">
      <Card className="p-4">
        <SectionTitle as="h1">{title}</SectionTitle>
        <EmptyState message={message} />
      </Card>
    </div>
  );
}
