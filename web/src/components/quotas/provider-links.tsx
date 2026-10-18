import { ChevronDown, ExternalLink } from "lucide-react"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"

type Links = components["schemas"]["Links"]
type LinkKey = keyof Links

interface ProviderLink {
  key: LinkKey
  label: string
  href: string
}

// The provider pages, in the order the card names them; dedup keeps the first
// label when two keys share one href.
const LINK_ORDER: { key: LinkKey; label: string }[] = [
  { key: "top_up", label: "Top up" },
  { key: "manage_subscription", label: "Manage subscription" },
  { key: "billing", label: "Billing" },
  { key: "usage", label: "Usage" },
  { key: "cancel", label: "Cancel subscription" },
]

/** The provider links in canonical order, deduped by href. */
function providerLinks(links: Links | undefined | null): ProviderLink[] {
  const seen = new Set<string>()
  const out: ProviderLink[] = []
  for (const { key, label } of LINK_ORDER) {
    const href = links?.[key]
    if (!href || seen.has(href)) continue
    seen.add(href)
    out.push({ key, label, href })
  }
  return out
}

/**
 * One primary provider link (by context: wallet or low balance → Top up,
 * renewal → Manage subscription, otherwise Usage; Cancel never leads) and the
 * rest in an "Open in provider" menu. `primaryKey` pins the alert kinds'
 * mapping: a key forces that link, `null` means no primary at all, and
 * leaving it out falls back to the context rule.
 */
export function ProviderLinks({
  links,
  wallet,
  renewal,
  primaryKey,
}: {
  links: Links | undefined | null
  /** A wallet meter or low balance: Top up leads. */
  wallet?: boolean
  /** A renewal is the subject: Manage subscription leads. */
  renewal?: boolean
  /** Forced primary (alert rows): a key, `null` for none, absent for context. */
  primaryKey?: LinkKey | null
}) {
  const all = providerLinks(links)
  if (all.length === 0) return null
  const primary =
    primaryKey !== undefined
      ? primaryKey
        ? all.find((l) => l.key === primaryKey)
        : undefined
      : all.find((l) => l.key === (wallet ? "top_up" : renewal ? "manage_subscription" : "usage"))
  const rest = primary ? all.filter((l) => l !== primary) : all
  return (
    <>
      {primary && (
        <Button asChild variant="outline" size="sm">
          <a href={primary.href} target="_blank" rel="noreferrer">
            {primary.label}
            <ExternalLink aria-hidden className="size-3" />
          </a>
        </Button>
      )}
      {rest.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" size="sm">
              Open in provider
              <ChevronDown aria-hidden className="size-3" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            {rest.map((link) => (
              <DropdownMenuItem key={link.key} asChild>
                <a href={link.href} target="_blank" rel="noreferrer">
                  {link.label}
                  <ExternalLink aria-hidden className="size-3" />
                </a>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </>
  )
}
