import { Bell, ChartColumn, Check, ChevronDown, CreditCard, Gauge, LayoutDashboard, LogOut, Monitor, Moon, Server, Settings, Sun, Waypoints } from "lucide-react"
import type { LucideIcon } from "lucide-react"
import { AnimatePresence, motion } from "motion/react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { query } from "@/api/client"
import type { components } from "@/api/schema"
import { useTheme, type Theme } from "@/components/theme"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import { Sidebar, SidebarContent, SidebarGroup, SidebarGroupContent, SidebarHeader, SidebarInset, SidebarMenu, SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarRail, SidebarTrigger } from "@/components/ui/sidebar"
import { APP_NAME, DEFAULT_SOURCE_URL } from "@/lib/brand"

type Me = components["schemas"]["Me"]

interface NavItem {
  to: string
  title: string
  icon: LucideIcon
}

const NAV: NavItem[] = [
  { to: "/", title: "Overview", icon: LayoutDashboard },
  { to: "/quotas", title: "Quotas", icon: Gauge },
  { to: "/accounts", title: "Accounts", icon: CreditCard },
  { to: "/usage", title: "Usage", icon: ChartColumn },
  { to: "/alerts", title: "Alerts", icon: Bell },
  { to: "/machines", title: "Machines", icon: Server },
]

const ADMIN_NAV: NavItem = { to: "/settings", title: "Settings", icon: Settings }

const TITLES: Record<string, string> = Object.fromEntries(
  [...NAV, ADMIN_NAV].map((item) => [item.to, item.title]),
)

const THEME_OPTIONS: { value: Theme; label: string; icon: LucideIcon }[] = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
]

function ThemeMenu() {
  const { theme, setTheme } = useTheme()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="Change theme">
          <Sun className="dark:hidden" />
          <Moon className="hidden dark:block" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {THEME_OPTIONS.map((option) => (
          <DropdownMenuItem key={option.value} onSelect={() => setTheme(option.value)}>
            <option.icon />
            {option.label}
            {theme === option.value && <Check className="ml-auto" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function UserMenu({ me }: { me: Me }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const logout = query.useMutation("post", "/api/auth/logout", {
    onSuccess: () => {
      queryClient.clear()
      navigate("/login", { replace: true })
    },
    onError: () => toast.error("Could not log out. Try again."),
  })
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className="gap-2 px-2">
          <span className="flex size-6 items-center justify-center rounded-full bg-muted text-xs font-medium">
            {(me.user.name || me.user.email).slice(0, 1).toUpperCase()}
          </span>
          <span className="hidden max-w-32 truncate sm:inline">{me.user.name || me.user.email}</span>
          <ChevronDown className="size-3.5 text-muted-foreground" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel className="font-normal">
          <div className="truncate text-sm font-medium">{me.user.name || me.user.email}</div>
          <div className="truncate text-xs text-muted-foreground">{me.user.email}</div>
          <div className="mt-1 text-xs capitalize text-muted-foreground">{me.user.role}</div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => logout.mutate()}>
          <LogOut />
          Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function AppShell() {
  const location = useLocation()
  const me = query.useQuery("get", "/api/me")
  const health = query.useQuery("get", "/healthz")
  const settings = query.useQuery("get", "/api/settings")
  const isAdmin = me.data?.user.role === "admin"
  const items = isAdmin ? [...NAV, ADMIN_NAV] : NAV
  const version = health.data?.version ?? ""
  const sourceUrl = settings.data?.source_url ?? DEFAULT_SOURCE_URL

  return (
    <SidebarProvider>
      <Sidebar collapsible="icon">
        <SidebarHeader>
          <div className="flex items-center gap-2 px-2 py-1.5">
            <div className="flex size-6 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground">
              <Waypoints className="size-4" />
            </div>
            <span className="truncate font-semibold group-data-[collapsible=icon]:hidden">{APP_NAME}</span>
          </div>
        </SidebarHeader>
        <SidebarContent>
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {items.map((item) => (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton asChild isActive={location.pathname === item.to} tooltip={item.title}>
                      <NavLink to={item.to}>
                        <item.icon />
                        <span>{item.title}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        </SidebarContent>
        <SidebarRail />
      </Sidebar>
      <SidebarInset>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-1 !h-4" />
          <div className="text-sm font-medium">{TITLES[location.pathname] ?? APP_NAME}</div>
          <div className="ml-auto flex items-center gap-1">
            <ThemeMenu />
            {me.data && <UserMenu me={me.data} />}
          </div>
        </header>
        <main className="flex-1">
          <AnimatePresence initial={false} mode="wait">
            <motion.div
              key={location.pathname}
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              transition={{ duration: 0.15 }}
            >
              <Outlet />
            </motion.div>
          </AnimatePresence>
        </main>
        <footer className="flex shrink-0 items-center gap-1.5 border-t px-6 py-3 text-xs text-muted-foreground">
          <span>{[APP_NAME, version].filter(Boolean).join(" ")}</span>
          <span aria-hidden>·</span>
          <a href={sourceUrl} target="_blank" rel="noreferrer" className="underline-offset-2 hover:underline">
            Source
          </a>
        </footer>
      </SidebarInset>
    </SidebarProvider>
  )
}
