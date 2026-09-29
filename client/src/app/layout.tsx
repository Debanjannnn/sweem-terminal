import type { Metadata } from "next";
import { Geist_Mono, Inter } from "next/font/google";
import "./globals.css";
import { SessionProvider } from "@/lib/session";
import { Toaster } from "@/components/Toaster";
import { MotionRoot } from "@/components/motion";

const inter = Inter({ variable: "--font-inter", subsets: ["latin"] });
const mono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: "sweem — tradable baskets on Monad",
  description: "Build, buy and automate token baskets on Kuru order books. Monad testnet.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`dark ${inter.variable} ${mono.variable} h-full`}
    >
      <body className={`min-h-full font-sans bg-background text-foreground antialiased`}>
        <MotionRoot>
          <SessionProvider>{children}</SessionProvider>
          <Toaster />
        </MotionRoot>
      </body>
    </html>
  );
}
