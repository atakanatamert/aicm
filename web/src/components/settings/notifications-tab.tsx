import { useState, type FormEvent } from "react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { ALERT_KINDS } from "@/components/alerts/alert-kinds"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { TableSkeleton } from "@/components/settings/fields"
import { messageOf } from "@/components/settings/helpers"
import { StatusDot } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { formatRelative } from "@/lib/format"

type Channel = components["schemas"]["NotificationChannel"]
type ChannelKind = components["schemas"]["ChannelKind"]
type Severity = components["schemas"]["Severity"]
type AlertKind = components["schemas"]["AlertKind"]

const KINDS: ChannelKind[] = ["webhook", "ntfy", "telegram", "discord", "slack"]

const KIND_LABEL: Record<ChannelKind, string> = {
  webhook: "Webhook",
  ntfy: "ntfy",
  telegram: "Telegram",
  discord: "Discord",
  slack: "Slack",
}

const SEVERITY_LABEL: Record<Severity, string> = {
  info: "info",
  warning: "warning",
  critical: "critical",
}

/** Per-kind secret/config fields, as ChannelKind describes them in the spec. */
const KIND_DOC: Record<ChannelKind, { text: string; href?: string }> = {
  webhook: {
    text: "The hub POSTs every alert as JSON to this URL.",
  },
  discord: {
    text: "Get this URL in your server settings under Integrations → Webhooks.",
    href: "https://support.discord.com/hc/en-us/articles/228383662-Intro-to-Webhooks",
  },
  slack: {
    text: "Create an incoming webhook for your workspace to get this URL.",
    href: "https://api.slack.com/messaging/webhooks",
  },
  ntfy: {
    text: "Subscribe to the topic in the ntfy app to receive the alerts.",
    href: "https://docs.ntfy.sh/subscribe/",
  },
  telegram: {
    text: "Create a bot with @BotFather to get the token; the chat id comes from a bot such as @userinfobot.",
    href: "https://core.telegram.org/bots/tutorial#obtain-your-bot-token",
  },
}

/** "warning and above · all alerts", or the restricted kinds by label. */
function filterSummary(minSeverity: Severity, kinds: AlertKind[]): string {
  const kindPart =
    kinds.length === 0
      ? "all alerts"
      : kinds.map((kind) => ALERT_KINDS.find((meta) => meta.kind === kind)?.label ?? kind).join(", ")
  return `${SEVERITY_LABEL[minSeverity]} and above · ${kindPart}`
}

function ChannelStatus({ channel }: { channel: Channel }) {
  const tone = channel.last_error ? "bad" : channel.last_sent_at ? "ok" : "idle"
  return (
    <StatusDot tone={tone} title={channel.last_error ?? undefined}>
      {tone === "bad"
        ? "delivery failed"
        : tone === "ok"
          ? `sent ${formatRelative(channel.last_sent_at as string)}`
          : "never sent"}
    </StatusDot>
  )
}

/** Ticked kinds restrict what the channel receives; none ticked means all. */
function selectionOf(kinds: AlertKind[] | undefined): Record<AlertKind, boolean> {
  const selected = {} as Record<AlertKind, boolean>
  for (const meta of ALERT_KINDS) selected[meta.kind] = kinds?.includes(meta.kind) ?? false
  return selected
}

export function NotificationsTab() {
  const channels = query.useQuery("get", "/api/notification-channels")
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Channel | undefined>()
  const [deleting, setDeleting] = useState<Channel | undefined>()
  // Test outcome per channel, shown inline next to its row's buttons.
  const [tests, setTests] = useState<Record<string, { state: "pending" | "sent" | "error"; message?: string }>>({})

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["get", "/api/notification-channels"] })
    void queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
  }

  const test = query.useMutation("post", "/api/notification-channels/{channelId}/test", {
    onSuccess: (_, variables) => {
      setTests((prev) => ({ ...prev, [variables.params.path.channelId]: { state: "sent" } }))
      invalidate()
    },
    onError: (err, variables) => {
      setTests((prev) => ({
        ...prev,
        [variables.params.path.channelId]: {
          state: "error",
          message: messageOf(err, "The channel refused the message."),
        },
      }))
      invalidate()
    },
  })
  const patch = query.useMutation("patch", "/api/notification-channels/{channelId}", {
    onSuccess: () => invalidate(),
    onError: (err) => toast.error(messageOf(err, "Could not update the channel.")),
  })
  const remove = query.useMutation("delete", "/api/notification-channels/{channelId}", {
    onSuccess: () => {
      invalidate()
      setDeleting(undefined)
      toast.success("Channel deleted.")
    },
    onError: (err) => toast.error(messageOf(err, "Could not delete the channel.")),
  })

  return (
    <section className="py-6">
      <Card>
        <CardContent className="px-0 pb-0">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pb-3 sm:px-6">
            <div>
              <h2 className="text-base leading-none font-semibold">Alert channels</h2>
              <p className="mt-1 text-sm text-muted-foreground">
                Besides email, alerts go to these channels. Email settings live in their own tab.
              </p>
            </div>
            <Button size="sm" onClick={() => setAdding(true)}>
              Add channel
            </Button>
          </div>
          {channels.isPending ? (
            <div className="px-4 py-4 sm:px-6">
              <TableSkeleton rows={2} />
            </div>
          ) : channels.isError ? (
            <p role="alert" className="px-4 py-4 text-sm text-destructive sm:px-6">
              {messageOf(channels.error, "Could not load the channels.")}
            </p>
          ) : channels.data.length === 0 ? (
            <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4 sm:px-6">
              <p className="text-sm text-muted-foreground">No channels yet — email still receives every alert.</p>
              <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
                Add channel
              </Button>
            </div>
          ) : (
            <ul>
              {channels.data.map((channel) => {
                const outcome = tests[channel.id]
                return (
                  <li key={channel.id} className="border-b px-4 py-3 last:border-b-0 sm:px-6">
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                      <Badge variant="outline">{KIND_LABEL[channel.kind]}</Badge>
                      <span className="text-sm font-medium">{channel.name}</span>
                      <span className="text-xs text-muted-foreground">
                        {filterSummary(channel.min_severity, channel.kinds)}
                      </span>
                      <ChannelStatus channel={channel} />
                      <div className="ml-auto flex flex-wrap items-center gap-2">
                        <Switch
                          checked={channel.enabled}
                          onCheckedChange={(enabled) =>
                            patch.mutate({ params: { path: { channelId: channel.id } }, body: { enabled } })
                          }
                          aria-label={`${channel.enabled ? "Disable" : "Enable"} ${channel.name}`}
                        />
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={outcome?.state === "pending"}
                          onClick={() => {
                            setTests((prev) => ({ ...prev, [channel.id]: { state: "pending" } }))
                            test.mutate({ params: { path: { channelId: channel.id } } })
                          }}
                        >
                          {outcome?.state === "pending" ? "Testing…" : "Test"}
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setEditing(channel)}>
                          Edit
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setDeleting(channel)}>
                          Delete
                        </Button>
                      </div>
                    </div>
                    {outcome?.state === "sent" && <p className="mt-2 text-xs text-muted-foreground">Sent.</p>}
                    {outcome?.state === "error" && (
                      <p role="alert" className="mt-2 text-xs text-destructive">
                        {outcome.message}
                      </p>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      {(adding || editing) && <ChannelDialog channel={editing} onClose={() => {
        setAdding(false)
        setEditing(undefined)
      }} />}

      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(open) => !open && setDeleting(undefined)}
        title="Delete channel"
        description={`Alerts stop going to ${deleting?.name ?? "this channel"}.`}
        confirmLabel="Delete"
        destructive
        pending={remove.isPending}
        onConfirm={() => deleting && remove.mutate({ params: { path: { channelId: deleting.id } } })}
      />
    </section>
  )
}

function ChannelDialog({ channel, onClose }: { channel?: Channel; onClose: () => void }) {
  const [kind, setKind] = useState<ChannelKind>(channel?.kind ?? "webhook")
  const [name, setName] = useState(channel?.name ?? "")
  const [server, setServer] = useState(channel?.config.server ?? "")
  const [topic, setTopic] = useState(channel?.config.topic ?? "")
  const [chatId, setChatId] = useState(channel?.config.chat_id ?? "")
  const [secret, setSecret] = useState("")
  const [minSeverity, setMinSeverity] = useState<Severity>(channel?.min_severity ?? "warning")
  const [selected, setSelected] = useState<Record<AlertKind, boolean>>(() => selectionOf(channel?.kinds))
  const [enabled, setEnabled] = useState(channel?.enabled ?? true)
  const [error, setError] = useState("")

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ["get", "/api/notification-channels"] })
    void queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
  }

  const create = query.useMutation("post", "/api/notification-channels", {
    onSuccess: () => {
      invalidate()
      toast.success("Channel added.")
      onClose()
    },
    onError: (err) => setError(messageOf(err, "Could not add the channel.")),
  })
  const update = query.useMutation("patch", "/api/notification-channels/{channelId}", {
    onSuccess: () => {
      invalidate()
      toast.success("Channel saved.")
      onClose()
    },
    onError: (err) => setError(messageOf(err, "Could not save the channel.")),
  })

  const config: Record<string, string> = {}
  if (kind === "ntfy") {
    if (server.trim() !== "") config.server = server.trim()
    config.topic = topic.trim()
  }
  if (kind === "telegram") config.chat_id = chatId.trim()

  // webhook, discord and slack carry their credential in the URL, telegram in
  // the bot token; on edit an empty field keeps the stored secret. The ntfy
  // token is optional.
  const secretHeld = channel !== undefined && channel.secret.set
  const secretValid = kind === "ntfy" || secretHeld || secret.trim() !== ""
  const kindValid =
    (kind !== "ntfy" || topic.trim() !== "") &&
    (kind !== "telegram" || chatId.trim() !== "") &&
    secretValid
  const valid = name.trim() !== "" && kindValid

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!valid) return
    setError("")
    const body = {
      name: name.trim(),
      config,
      min_severity: minSeverity,
      kinds: ALERT_KINDS.filter((meta) => selected[meta.kind]).map((meta) => meta.kind),
      enabled,
    }
    if (channel) {
      update.mutate({
        params: { path: { channelId: channel.id } },
        body: secret.trim() !== "" ? { ...body, secret: secret.trim() } : body,
      })
    } else {
      create.mutate({
        body: { kind, ...body, ...(secret.trim() !== "" ? { secret: secret.trim() } : {}) },
      })
    }
  }

  const doc = KIND_DOC[kind]
  const keepHint = secretHeld ? `leave empty to keep ••••${channel?.secret.last4 ?? "…"}` : undefined

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{channel ? "Edit channel" : "Add channel"}</DialogTitle>
          <DialogDescription>Alerts go out when a rule fires and the severity matches.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4">
          <div className="grid gap-2">
            <Label htmlFor="channel-kind">Kind</Label>
            {channel ? (
              <Input id="channel-kind" value={KIND_LABEL[channel.kind]} disabled />
            ) : (
              <Select value={kind} onValueChange={(value) => setKind(value as ChannelKind)}>
                <SelectTrigger id="channel-kind" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {KINDS.map((k) => (
                    <SelectItem key={k} value={k}>
                      {KIND_LABEL[k]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="channel-name">Name</Label>
            <Input
              id="channel-name"
              placeholder="Team chat"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          {(kind === "webhook" || kind === "discord" || kind === "slack") && (
            <div className="grid gap-2">
              <Label htmlFor="channel-secret">URL</Label>
              <Input
                id="channel-secret"
                type="password"
                autoComplete="new-password"
                required={!secretHeld}
                placeholder={keepHint ?? "https://…"}
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">
                {doc.text}{" "}
                {doc.href && (
                  <a href={doc.href} target="_blank" rel="noreferrer" className="underline underline-offset-2">
                    How to get this URL.
                  </a>
                )}
              </p>
            </div>
          )}

          {kind === "ntfy" && (
            <>
              <div className="grid gap-2">
                <Label htmlFor="ntfy-server">Server</Label>
                <Input
                  id="ntfy-server"
                  type="url"
                  placeholder="https://ntfy.sh"
                  value={server}
                  onChange={(e) => setServer(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="ntfy-topic">Topic</Label>
                <Input id="ntfy-topic" required value={topic} onChange={(e) => setTopic(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  {doc.text}{" "}
                  {doc.href && (
                    <a href={doc.href} target="_blank" rel="noreferrer" className="underline underline-offset-2">
                      ntfy docs.
                    </a>
                  )}
                </p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="ntfy-secret">Access token</Label>
                <Input
                  id="ntfy-secret"
                  type="password"
                  autoComplete="new-password"
                  placeholder={keepHint ?? "optional"}
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                />
              </div>
            </>
          )}

          {kind === "telegram" && (
            <>
              <div className="grid gap-2">
                <Label htmlFor="telegram-secret">Bot token</Label>
                <Input
                  id="telegram-secret"
                  type="password"
                  autoComplete="new-password"
                  required={!secretHeld}
                  placeholder={keepHint ?? "123456:ABC-…"}
                  value={secret}
                  onChange={(e) => setSecret(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="telegram-chat">Chat id</Label>
                <Input id="telegram-chat" required value={chatId} onChange={(e) => setChatId(e.target.value)} />
                <p className="text-xs text-muted-foreground">
                  {doc.text}{" "}
                  {doc.href && (
                    <a href={doc.href} target="_blank" rel="noreferrer" className="underline underline-offset-2">
                      Telegram bot docs.
                    </a>
                  )}
                </p>
              </div>
            </>
          )}

          <div className="grid gap-2">
            <Label htmlFor="channel-severity">Minimum severity</Label>
            <Select value={minSeverity} onValueChange={(value) => setMinSeverity(value as Severity)}>
              <SelectTrigger id="channel-severity" className="w-full sm:w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {(Object.keys(SEVERITY_LABEL) as Severity[]).map((severity) => (
                  <SelectItem key={severity} value={severity}>
                    {SEVERITY_LABEL[severity]} and above
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="grid gap-2">
            <Label>Alert kinds</Label>
            <div className="grid gap-2 sm:grid-cols-2">
              {ALERT_KINDS.map((meta) => (
                <label key={meta.kind} className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={selected[meta.kind]}
                    onCheckedChange={(checked) =>
                      setSelected((prev) => ({ ...prev, [meta.kind]: checked === true }))
                    }
                  />
                  {meta.label}
                </label>
              ))}
            </div>
            <p className="text-xs text-muted-foreground">Untick everything to send every alert kind.</p>
          </div>

          <label className="flex items-center gap-2 text-sm">
            <Switch checked={enabled} onCheckedChange={setEnabled} aria-label="Channel enabled" />
            Enabled
          </label>

          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!valid || create.isPending || update.isPending}>
              {create.isPending || update.isPending ? "Saving…" : channel ? "Save changes" : "Add channel"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
