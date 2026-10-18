import { Navigate, Outlet, RouterProvider, createBrowserRouter, useLocation } from "react-router"
import { errorOf, query } from "@/api/client"
import { AppShell } from "@/components/app-shell"
import Accounts from "@/routes/accounts"
import AccountDetail from "@/routes/account-detail"
import Alerts from "@/routes/alerts"
import Login from "@/routes/login"
import Machines from "@/routes/machines"
import Overview from "@/routes/overview"
import Quotas from "@/routes/quotas"
import Settings from "@/routes/settings"
import Setup from "@/routes/setup"
import Usage from "@/routes/usage"

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

const router = createBrowserRouter([
  {
    element: <RequireAuth />,
    children: [
      {
        element: <AppShell />,
        children: [
          { path: "/", element: <Overview /> },
          { path: "/quotas", element: <Quotas /> },
          { path: "/accounts", element: <Accounts /> },
          { path: "/accounts/:id", element: <AccountDetail /> },
          { path: "/usage", element: <Usage /> },
          { path: "/alerts", element: <Alerts /> },
          { path: "/machines", element: <Machines /> },
          { path: "/settings", element: <Settings /> },
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
