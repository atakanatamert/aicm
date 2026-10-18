import { QueryClient } from "@tanstack/react-query"
import createClient from "openapi-fetch"
import createQueryClient from "openapi-react-query"
import type { components, paths } from "./schema"

// Same-origin API: cookies ride along, no hand-written fetch anywhere.
export const api = createClient<paths>({
  baseUrl: "",
  credentials: "include",
})

// TanStack's cache; query.useQuery / query.useMutation use it via context.
export const queryClient = new QueryClient()

// Typed hooks over the fetch client.
export const query = createQueryClient(api)

type ErrorEnvelope = components["schemas"]["Error"]

// Narrow a query/mutation error to the spec's error envelope, if it is one.
// Network failures and aborted requests are not.
export function errorOf(err: unknown): ErrorEnvelope | undefined {
  if (typeof err === "object" && err !== null && "error" in err) {
    const envelope = err as ErrorEnvelope
    if (typeof envelope.error?.code === "string") return envelope
  }
  return undefined
}
