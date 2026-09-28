import {
  Bell,
  Command,
  CreditCard,
  Globe2,
  Images,
  LayoutDashboard,
  Monitor,
  Palette,
  Settings,
  ShieldCheck,
  UserCog,
  Users,
  UsersRound,
  Wrench,
} from 'lucide-react'
import { type SidebarData } from '../types'

// Keep the navigation limited to real ImageHub workflows. Template demo
// pages are intentionally not reachable from the product sidebar.
export const sidebarData: SidebarData = {
  user: {
    name: 'ImageHub user',
    email: '',
    avatar: '/avatars/shadcn.jpg',
  },
  teams: [{ name: 'Personal', logo: Command, plan: 'free' }],
  navGroups: [
    {
      title: 'Library',
      items: [
        { title: 'Dashboard', url: '/dashboard', icon: LayoutDashboard },
        { title: 'Image library', url: '/images', icon: Images },
        { title: 'Teams', url: '/teams', icon: UsersRound },
        { title: 'Billing', url: '/billing', icon: CreditCard },
        { title: 'Custom domains', url: '/domains', icon: Globe2 },
      ],
    },
    {
      title: 'Administration',
      roles: ['admin'],
      items: [
        { title: 'All media', url: '/admin-images', icon: Images },
        { title: 'Users', url: '/users', icon: Users },
        { title: 'System settings', url: '/settings/system', icon: ShieldCheck },
      ],
    },
    {
      title: 'Personal',
      items: [
        {
          title: 'Settings',
          icon: Settings,
          items: [
            { title: 'Profile', url: '/settings', icon: UserCog },
            { title: 'Account', url: '/settings/account', icon: Wrench },
            { title: 'Appearance', url: '/settings/appearance', icon: Palette },
            { title: 'Notifications', url: '/settings/notifications', icon: Bell },
            { title: 'Display', url: '/settings/display', icon: Monitor },
          ],
        },
      ],
    },
  ],
}
