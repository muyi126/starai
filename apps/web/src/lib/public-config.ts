import type { SystemConfig } from "@starai/shared-types";

// Server requests can bypass public DNS/CDN/TLS; the browser keeps its public API URL.
export const API_URL = process.env.INTERNAL_API_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export type PublicSystemConfig = Partial<SystemConfig>;

export async function getPublicSystemConfig(): Promise<PublicSystemConfig> {
  try {
    const res = await fetch(`${API_URL}/api/system-configs/public`, { next: { revalidate: 60 }, signal: AbortSignal.timeout(5_000) });
    if (!res.ok) return {};
    const json = await res.json();
    return (json?.data || {}) as PublicSystemConfig;
  } catch {
    return {};
  }
}
