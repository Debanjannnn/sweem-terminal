import { Rail } from "@/components/shell/Rail";
import { TopBar } from "@/components/shell/TopBar";
import { PageTransition } from "@/components/motion";

export default function TerminalLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="flex h-full bg-background text-foreground">
      <Rail />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar />
        <PageTransition>{children}</PageTransition>
      </div>
    </div>
  );
}
