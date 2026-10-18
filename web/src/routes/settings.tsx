import { PageHeader } from "@/components/page-header"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useMe } from "@/hooks/use-me"
import { AuditTab } from "@/components/settings/audit-tab"
import { EmailTab } from "@/components/settings/email-tab"
import { GroupsTab } from "@/components/settings/groups-tab"
import { OrganizationTab } from "@/components/settings/organization-tab"
import { ProfileTab } from "@/components/settings/profile-tab"
import { UsersTab } from "@/components/settings/users-tab"

export default function Settings() {
  const { isAdmin } = useMe()
  return (
    <>
      <PageHeader
        title="Settings"
        description="Your password and API tokens; admins also manage users, groups, the organization, email and the audit log."
      />
      <Tabs defaultValue="profile" className="px-6">
        <div className="overflow-x-auto pb-1 pt-4">
          <TabsList>
            <TabsTrigger value="profile">Profile</TabsTrigger>
            {isAdmin && <TabsTrigger value="users">Users</TabsTrigger>}
            {isAdmin && <TabsTrigger value="groups">Groups</TabsTrigger>}
            {isAdmin && <TabsTrigger value="organization">Organization</TabsTrigger>}
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
            <TabsContent value="email">
              <EmailTab />
            </TabsContent>
            <TabsContent value="audit">
              <AuditTab />
            </TabsContent>
          </>
        )}
      </Tabs>
    </>
  )
}
