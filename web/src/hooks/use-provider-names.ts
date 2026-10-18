import { useMemo } from "react"

import { query } from "@/api/client"

/**
 * Display names from the compiled-in provider catalog, keyed by provider id.
 * Unknown ids fall back to the id itself.
 */
export function useProviderNames(): (providerId: string) => string {
  const providers = query.useQuery("get", "/api/providers", undefined, { staleTime: Infinity })
  return useMemo(() => {
    const names: Record<string, string> = {}
    for (const p of providers.data ?? []) names[p.id] = p.name
    return (providerId: string) => names[providerId] ?? providerId
  }, [providers.data])
}
