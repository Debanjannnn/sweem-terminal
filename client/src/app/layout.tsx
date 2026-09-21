import type { Metadata } from "next";
import { Poppins } from "next/font/google";
import { Toaster } from "sonner";
import { MotionProvider } from "@/components/motion";
import { QueryProvider } from "@/lib/query";
import { SessionProvider } from "@/lib/session";
import "./globals.css";

const poppins = Poppins({
  variable: "--font-poppins",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700"],
});

export const metadata: Metadata = {
  title: "sweem terminal",
  description: "Trading terminal on Monad.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className={`${poppins.variable} h-full`}>
      <body className={`${poppins.className} h-full antialiased`}>
        <SessionProvider>
          <QueryProvider>
            <MotionProvider>{children}</MotionProvider>
          </QueryProvider>
        </SessionProvider>
        <Toaster
          theme="dark"
          position="bottom-right"
          style={
            {
              "--normal-bg": "var(--popover)",
              "--normal-text": "var(--popover-foreground)",
              "--normal-border": "var(--border)",
              "--border-radius": "var(--radius)",
            } as React.CSSProperties
          }
        />
      </body>
    </html>
  );
}
