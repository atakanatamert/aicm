import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

type EnrollTokenCreated = components["schemas"]["EnrollTokenCreated"]
type CommandKey = keyof EnrollTokenCreated["commands"]

/**
 * What the agent does on the machine it runs on, in one line. Shared by the
 * GetStarted panel and the Accounts, Quotas and Machines empty states so
 * import reads the same everywhere.
 */
export const AGENT_READS_TEXT =
  "It reads Claude Code, Codex and omp on this computer and sends usage and limits here. It wraps Claude Code's status line to see your limits (undo: aicm-agent statusline uninstall). Your logins stay on this computer."

// One tab per install route, in the order the card names them.
const COMMAND_TABS: { key: CommandKey; label: string }[] = [
  { key: "sh", label: "macOS / Linux" },
  { key: "brew", label: "Homebrew" },
  { key: "powershell", label: "Windows" },
  { key: "docker", label: "Docker" },
]

function CommandBlock({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)

  const copy = () => {
    navigator.clipboard
      .writeText(command)
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      })
      .catch(() => toast.error("Could not copy. Select the command and copy it by hand."))
  }

  return (
    <div className="grid gap-1.5">
      <div className="flex justify-end">
        <Button variant="outline" size="xs" onClick={copy}>
          {copied ? <Check /> : <Copy />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs leading-relaxed">
        <code>{command}</code>
      </pre>
    </div>
  )
}

/**
 * The four install commands for one enrollment token, with Copy buttons and
 * the shown-once note. The tab for the visitor's OS is preselected.
 */
export function EnrollCommands({ created }: { created: EnrollTokenCreated }) {
  const commands = created.commands
  return (
    <div className="grid min-w-0 gap-1.5">
      {/* The visitor's OS, from the user agent: PowerShell on Windows, sh elsewhere. */}
      <Tabs defaultValue={/Windows/i.test(navigator.userAgent) ? "powershell" : "sh"} className="min-w-0">
        <TabsList>
          {COMMAND_TABS.map(
            ({ key, label }) =>
              commands[key] && (
                <TabsTrigger key={key} value={key}>
                  {label}
                </TabsTrigger>
              ),
          )}
        </TabsList>
        {COMMAND_TABS.map(
          ({ key }) =>
            commands[key] && (
              <TabsContent key={key} value={key} className="min-w-0">
                <CommandBlock command={commands[key] ?? ""} />
              </TabsContent>
            ),
        )}
      </Tabs>
      <p className="text-xs text-muted-foreground">The token is shown once — copy it now.</p>
    </div>
  )
}
