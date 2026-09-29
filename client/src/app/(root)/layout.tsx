import { Suspense } from "react";
import { Navbar } from "@/components/Navbar";

export default function RootGroupLayout({ children }: LayoutProps<"/">) {
  return (
    <div className="flex min-h-full w-full flex-col items-center">
      <Suspense>
        <Navbar />
      </Suspense>
      <div className="w-full max-w-[1400px]">{children}</div>
    </div>
  );
}
