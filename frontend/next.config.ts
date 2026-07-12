import type { NextConfig } from "next";

// All browser traffic goes through this rewrite to the Go API — the frontend
// never talks to the Python service or the database (CLAUDE.md boundary),
// and same-origin requests avoid CORS entirely.
const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${API_URL}/:path*` }];
  },
};

export default nextConfig;
