import type { Metadata } from "next";
import type { ReactNode } from "react";
import "./globals.css";

export const metadata: Metadata = {
  title: "Personal Context",
  description: "Personal Context web workspace"
};

type RootLayoutProps = {
  children: ReactNode;
};

/**
 * Provides the global HTML shell for all app-router routes.
 *
 * @param children - Route content rendered by Next.js.
 * @returns The root document shell.
 */
export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
