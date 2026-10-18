import { query } from "@/api/client"

/**
 * The signed-in user and what their role allows. Screens hide edit controls
 * the role can't use; the API enforces the same rules (x-aicm-role).
 */
export function useMe() {
  const me = query.useQuery("get", "/api/me")
  const role = me.data?.user.role
  return {
    me: me.data,
    isLoading: me.isLoading,
    canEdit: role === "editor" || role === "admin",
    isAdmin: role === "admin",
    displayCurrency: me.data?.org.display_currency ?? "USD",
  }
}
