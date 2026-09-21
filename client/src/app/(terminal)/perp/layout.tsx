import { SubNav } from "@/components/shell/SubNav";

export default function PerpLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <SubNav />
      {children}
    </>
  );
}
