import Link from "next/link";

export default function Home() {
  return (
    <div className="w-full space-y-6 px-4 py-16">
      <h1 className="text-4xl font-bold tracking-tight">make your money sweem</h1>
      <p className="max-w-lg text-muted-foreground">Tradable token baskets on Monad, filled on Kuru order books.</p>
      <Link href="/invest" className="inline-flex rounded-full bg-primary px-6 py-2.5 text-sm font-medium text-primary-foreground">
        Explore baskets
      </Link>
    </div>
  );
}
