import { toast } from "sonner"
import { errorOf } from "@/api/client"

/** Server error message, or a fallback for network and other failures. */
export function messageOf(err: unknown, fallback: string): string {
  return errorOf(err)?.error.message ?? fallback
}

/**
 * Mutation errors as toasts. The 409 `conflict` code (e.g. demoting or
 * disabling the last admin) gets a heading so it is impossible to miss.
 */
export function toastMutationError(err: unknown, fallback: string): void {
  const envelope = errorOf(err)
  if (envelope?.error.code === "conflict") {
    toast.error("Not allowed", { description: envelope.error.message, duration: 10000 })
  } else {
    toast.error(envelope?.error.message ?? fallback, { duration: 8000 })
  }
}

/** Copy to the clipboard with a confirmation; manual fallback if blocked. */
export async function copyText(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value)
    toast("Copied to clipboard.")
  } catch {
    toast.error("Could not copy — select the text and copy it by hand.")
  }
}

/** An unambiguous alphabet: no 0/O and no 1/l/I. */
const ALPHABET = "23456789abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"

/** A random password of at least 12 characters (the API's minimum). */
export function generatePassword(length = 16): string {
  const bytes = crypto.getRandomValues(new Uint32Array(length))
  let out = ""
  for (let i = 0; i < length; i++) out += ALPHABET[bytes[i] % ALPHABET.length]
  return out
}

/** How to point the agent's MCP server at the hub with a personal token. */
export const MCP_HINT = "claude mcp add aicm --env AICM_TOKEN=… -- aicm-agent mcp"

/** Currencies offered outright in the Organization tab; anything else goes into free entry. */
export const COMMON_CURRENCIES: { code: string; name: string }[] = [
  { code: "USD", name: "US dollar" },
  { code: "EUR", name: "Euro" },
  { code: "GBP", name: "British pound" },
  { code: "JPY", name: "Japanese yen" },
  { code: "CNY", name: "Chinese yuan" },
  { code: "CHF", name: "Swiss franc" },
  { code: "CAD", name: "Canadian dollar" },
  { code: "AUD", name: "Australian dollar" },
  { code: "NZD", name: "New Zealand dollar" },
  { code: "SGD", name: "Singapore dollar" },
  { code: "HKD", name: "Hong Kong dollar" },
  { code: "KRW", name: "South Korean won" },
  { code: "INR", name: "Indian rupee" },
  { code: "BRL", name: "Brazilian real" },
  { code: "MXN", name: "Mexican peso" },
  { code: "SEK", name: "Swedish krona" },
  { code: "NOK", name: "Norwegian krone" },
  { code: "DKK", name: "Danish krone" },
  { code: "PLN", name: "Polish złoty" },
  { code: "CZK", name: "Czech koruna" },
  { code: "TRY", name: "Turkish lira" },
  { code: "ZAR", name: "South African rand" },
]
