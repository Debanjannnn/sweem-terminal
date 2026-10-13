import type { NextConfig } from "next";

// No `output: "standalone"`: that is for self-hosting in a container, and it
// omits the file-tracing manifests Vercel's builder expects. The client is
// deployed to Vercel; docker-compose does not build it.
const nextConfig: NextConfig = {
  reactCompiler: true,
  // strategies were renamed baskets; :path* also matches bare /earn
  redirects: async () => [{ source: "/earn/:path*", destination: "/baskets/:path*", permanent: true }],
};

export default nextConfig;
