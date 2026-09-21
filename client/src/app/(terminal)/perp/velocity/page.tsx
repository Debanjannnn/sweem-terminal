import { Workspace } from "@/components/shell/Workspace";

export default function Page() {
  return (
    <Workspace
      idPrefix="sweem:vol:"
      emptyMessage="No velocity market has been published by the oracle yet."
    />
  );
}
