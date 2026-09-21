import { Workspace } from "@/components/shell/Workspace";

export default function Page() {
  return (
    <Workspace
      venue="perpl"
      emptyMessage="No Perpl markets are listed for this chain right now."
    />
  );
}
