import { Reveal } from "@/components/motion";

// remounts per route segment → every page fades/rises in
export default function Template({ children }: { children: React.ReactNode }) {
  return <Reveal>{children}</Reveal>;
}
