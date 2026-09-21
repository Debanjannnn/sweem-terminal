import { Workspace } from "@/components/shell/Workspace";

export default function Page() {
  return (
    <Workspace
      idPrefix="sweem:pow:"
      emptyMessage="No power market has been published by the oracle yet."
    />
  );
}
