import { Suspense } from "react";
import { HomePage } from "@/components/home/HomePage";

export default function Home() {
  return (
    <Suspense>
      <HomePage />
    </Suspense>
  );
}
