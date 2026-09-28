import type { Metadata } from "next";
import { Poppins } from "next/font/google";
import "./globals.css";
import { SessionProvider } from "@/lib/session";
import { Toaster } from "@/components/Toaster";

const poppins = Poppins({
  variable: "--font-geist-sans",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
});

export const metadata: Metadata = {
  title: "sweem — tradable baskets on Monad",
  description: "Build, buy and automate token baskets on Kuru order books. Monad testnet.",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`dark ${poppins.variable} h-full`}
    >
      <body className={`${poppins.className} min-h-full bg-background text-foreground antialiased`}>
        <SessionProvider>{children}</SessionProvider>
        <Toaster />
      </body>
    </html>
  );
}
