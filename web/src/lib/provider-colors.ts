/**
 * One colour per provider id, identical in both themes and picked to read on
 * light and dark card backgrounds. Charts and provider dots use this so a
 * provider keeps its colour everywhere, whatever its rank.
 */
export const PROVIDER_COLORS: Record<string, string> = {
  anthropic: "#d97757",
  claude: "#d97757",
  openai: "#10a37f",
  chatgpt: "#10a37f",
  zai: "#8b5cf6",
  copilot: "#0969da",
  openrouter: "#0ea5e9",
  deepseek: "#4d6bfe",
  moonshot: "#db2777",
  xai: "#84cc16",
  "google-ai-studio": "#4285f4",
  "google-one": "#ea4335",
  supergrok: "#38bdf8",
  cursor: "#2563eb",
  mistral: "#f97316",
  together: "#06b6d4",
  perplexity: "#14b8a6",
  cohere: "#c026d3",
  groq: "#f55036",
  fireworks: "#e11d48",
}

/** The provider's colour, or a neutral grey for ids outside the catalog. */
export function providerColor(providerId: string): string {
  return PROVIDER_COLORS[providerId] ?? "var(--muted-foreground)"
}
