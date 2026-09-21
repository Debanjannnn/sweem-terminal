import { Workspace } from "@/components/shell/Workspace";

export default function Page() {
  return (
    <Workspace
      venue="kuru"
      emptyMessage="No Kuru spot markets are listed for this chain right now."
    />
  );
}
