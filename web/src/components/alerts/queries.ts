import { query, queryClient } from "@/api/client"

/**
 * Accounts visible to the caller, including archived ones so alert rows keep
 * their names. Viewers and editors only ever see their granted accounts.
 */
export function useAccounts() {
  return query.useQuery("get", "/api/accounts", { params: { query: { include_archived: true } } })
}

/** Invalidate the inbox pages, the unread badge and every rule query. */
export function invalidateAlertQueries() {
  void queryClient.invalidateQueries({ queryKey: ["get", "/api/alerts"] })
  void queryClient.invalidateQueries({ queryKey: ["get", "/api/alert-rules"] })
}
