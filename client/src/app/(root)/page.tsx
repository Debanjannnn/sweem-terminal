import { Suspense } from "react";
import { InvestPage } from "@/components/invest/InvestPage";

export default function Home() {
  return (
    <Suspense>
      <InvestPage />
    </Suspense>
  );
}
