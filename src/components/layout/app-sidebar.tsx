import { useEffect, useState } from 'react'
import { useLayout } from '@/context/layout-provider'
import { Command } from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarRail,
} from '@/components/ui/sidebar'
// import { AppTitle } from './app-title'
import { sidebarData } from './data/sidebar-data'
import { NavGroup } from './nav-group'
import { NavUser } from './nav-user'
import { TeamSwitcher } from './team-switcher'
import { getCurrentUser, listTeams, type Team } from '@/lib/imagehub-api'
import { type NavItem } from './types'
import { useI18n } from '@/lib/i18n'

export function AppSidebar() {
  const { t } = useI18n()
  const { collapsible, variant } = useLayout()
  const [profile, setProfile] = useState({ name: 'ImageHub user', email: '', avatar: '/avatars/shadcn.jpg' })
  const [role, setRole] = useState('user')
  const [teams, setTeams] = useState<Team[]>([])
  useEffect(() => { void Promise.all([getCurrentUser(), listTeams()]).then(([user, nextTeams]) => { setProfile({ name: user.display_name || user.email.split('@')[0], email: user.email, avatar: '/avatars/shadcn.jpg' }); setRole(user.role); setTeams(nextTeams) }).catch(() => undefined) }, [])
  const switcherTeams = (teams.length ? teams : [{ id: 'personal', name: 'Personal', slug: 'personal', role: 'owner', quota_bytes: 0, used_bytes: 0, plan_code: 'free' }]).map((team) => ({ name: team.name, logo: Command, plan: team.plan_code }))
  const visibleNavGroups = sidebarData.navGroups.map((group) => ({
    ...group,
    items: group.items.flatMap<NavItem>((item) => {
      if (role !== 'admin' && (item.title === 'Users' || item.title === 'All media')) return []
      if (item.title !== 'Settings' || !item.items) return [item]
      return [{ ...item, items: item.items.filter((subItem) => role === 'admin' || subItem.title !== 'System settings') }]
    }),
  })).filter((group) => group.items.length > 0)
  const labels: Record<string, string> = { Library: 'nav.library', Administration: 'nav.administration', Support: 'nav.support', Dashboard: 'nav.dashboard', 'Image library': 'nav.images', 'All media': 'nav.allMedia', Teams: 'nav.teams', Billing: 'nav.billing', 'Custom domains': 'nav.domains', Users: 'nav.users', Settings: 'nav.settings', Profile: 'nav.profile', Account: 'nav.account', Appearance: 'nav.appearance', Notifications: 'nav.notifications', Display: 'nav.display', 'System settings': 'nav.system', 'Help Center': 'nav.help' }
  const translatedNavGroups = visibleNavGroups.map((group) => ({ ...group, title: t(labels[group.title] ?? group.title, group.title), items: group.items.map((item) => ({ ...item, title: t(labels[item.title] ?? item.title, item.title), items: item.items?.map((subItem) => ({ ...subItem, title: t(labels[subItem.title] ?? subItem.title, subItem.title) })) })) }))
  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <SidebarHeader>
        <TeamSwitcher teams={switcherTeams} />

        {/* Replace <TeamSwitch /> with the following <AppTitle />
         /* if you want to use the normal app title instead of TeamSwitch dropdown */}
        {/* <AppTitle /> */}
      </SidebarHeader>
      <SidebarContent>
        {translatedNavGroups.map((props) => (
          <NavGroup key={props.title} {...props} />
        ))}
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={profile} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
