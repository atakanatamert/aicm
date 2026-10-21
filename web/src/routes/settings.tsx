import { useSearchParams } from "react-router"
import { PageHeader } from "@/components/page-header"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useMe } from "@/hooks/use-me"
import { AuditTab } from "@/components/settings/audit-tab"
import { EmailTab } from "@/components/settings/email-tab"
import { GroupsTab } from "@/components/settings/groups-tab"
import { NotificationsTab } from "@/components/settings/notifications-tab"
import { OrganizationTab } from "@/components/settings/organization-tab"
import { ProfileTab } from "@/components/settings/profile-tab"
import { UsersTab } from "@/components/settings/users-tab"

// Tab keys the URL understands; anything else falls back to Profile.
const TAB_VALUES = ["profile", "users", "groups", "organization", "notifications", "email", "audit"] as const
type TabValue = (typeof TAB_VALUES)[number]

function tabOf(raw: string | null): TabValue {
  return (TAB_VALUES as readonly string[]).includes(raw ?? "") ? (raw as TabValue) : "profile"
}

export default function Settings() {
  const { isAdmin } = useMe()
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = tabOf(searchParams.get("tab"))
  return (
    <>
      <PageHeader
        title="Settings"
        description="Your password and API tokens; admins also manage users, groups, the organization, alert channels, email and the audit log."
      />
      <div className="mx-auto w-full max-w-6xl px-4 sm:px-6">
        <Tabs
          value={tab}
          onValueChange={(value) => setSearchParams(value === "profile" ? {} : { tab: value })}
        >
          <div className="pb-1 pt-4">
            <TabsList className="h-auto flex-wrap justify-start">
              <TabsTrigger value="profile">Profile</TabsTrigger>
              {isAdmin && <TabsTrigger value="users">Users</TabsTrigger>}
              {isAdmin && <TabsTrigger value="groups">Groups</TabsTrigger>}
              {isAdmin && <TabsTrigger value="organization">Organization</TabsTrigger>}
              {isAdmin && <TabsTrigger value="notifications">Notifications</TabsTrigger>}
              {isAdmin && <TabsTrigger value="email">Email</TabsTrigger>}
              {isAdmin && <TabsTrigger value="audit">Audit log</TabsTrigger>}
            </TabsList>
          </div>
          <TabsContent value="profile">
            <ProfileTab />
          </TabsContent>
          {isAdmin && (
            <>
              <TabsContent value="users">
                <UsersTab />
              </TabsContent>
              <TabsContent value="groups">
                <GroupsTab />
              </TabsContent>
              <TabsContent value="organization">
                <OrganizationTab />
              </TabsContent>
              <TabsContent value="notifications">
                <NotificationsTab />
              </TabsContent>
              <TabsContent value="email">
                <EmailTab />
              </TabsContent>
              <TabsContent value="audit">
                <AuditTab />
              </TabsContent>
            </>
          )}
        </Tabs>
      </div>
    </>
  )
}
