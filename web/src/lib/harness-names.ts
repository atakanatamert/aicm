// Harness ids as the agent reports them, in the user's words. Anything not
// listed falls back to the id at the call site: HARNESS_NAMES[harness] ?? harness.
export const HARNESS_NAMES: Record<string, string> = {
  claudecode: "Claude Code",
  codex: "Codex",
  omp: "omp",
}
