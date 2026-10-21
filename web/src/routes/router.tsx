import { Navigate, Outlet, RouterProvider, createBrowserRouter, useLocation } from "react-router"
import { errorOf, query } from "@/api/client"
import { AppShell } from "@/components/app-shell"
import Login from "@/routes/login"
import Setup from "@/routes/setup"

function AuthLoading({ children }: { children?: string }) {
  return <div className="grid min-h-svh place-items-center text-sm text-muted-foreground">{children ?? "Loading…"}</div>
}

function SetupRetry() {
  return (
    <AuthLoading>
      Could not reach the hub. Check that it is running and reload.
    </AuthLoading>
  )
}

/**
 * Runs before every screen: first-run setup wins, then a logged-out visitor
 * lands on /login with the original path in `next`.
 */
function RequireAuth() {
  const location = useLocation()
  const setup = query.useQuery("get", "/api/setup", undefined, { retry: false })
  const me = query.useQuery("get", "/api/me", undefined, { retry: false })

  if (setup.isPending || me.isPending) return <AuthLoading />
  if (setup.isError) return <SetupRetry />
  if (setup.data.needed) return <Navigate to="/setup" replace />

  if (errorOf(me.error)?.error.code === "unauthorized") {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname)}`} replace />
  }
  if (me.isError) return <SetupRetry />
  return <Outlet />
}

// Each screen loads on first visit, keeping the first download small.
const screen = (load: () => Promise<{ default: React.ComponentType }>) => async () => ({ Component: (await load()).default })

const router = createBrowserRouter([
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { path: "/", lazy: screen(() => import("@/routes/overview")) },
          { path: "/quotas", lazy: screen(() => import("@/routes/quotas")) },
          { path: "/accounts", lazy: screen(() => import("@/routes/accounts")) },
          { path: "/accounts/:id", lazy: screen(() => import("@/routes/account-detail")) },
          { path: "/usage", lazy: screen(() => import("@/routes/usage")) },
          { path: "/alerts", lazy: screen(() => import("@/routes/alerts")) },
          { path: "/machines", lazy: screen(() => import("@/routes/machines")) },
          { path: "/settings", lazy: screen(() => import("@/routes/settings")) },
        ],
      },
    ],
  },
  { path: "/setup", element: <Setup /> },
  { path: "/login", element: <Login /> },
  { path: "*", element: <Navigate to="/" replace /> },
])

export function AppRoutes() {
  return <RouterProvider router={router} />
}
