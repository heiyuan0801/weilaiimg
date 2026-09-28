import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { getCurrentUser, ImageHubApiError } from '@/lib/imagehub-api'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ location }) => {
    const currentAuth = useAuthStore.getState().auth
    // The sign-in form has already verified the server session. Reusing that
    // in-memory state prevents a route preload from logging the user out while
    // navigating between protected pages such as Dashboard and Teams.
    if (currentAuth.user) return
    try {
      const user = await getCurrentUser()
      const auth = useAuthStore.getState().auth
      auth.setUser({ accountNo: user.id, email: user.email, role: [user.role], exp: Date.now() + 24 * 60 * 60 * 1000 })
      auth.setAccessToken('cookie-session')
    }
    catch (error) {
      // A lost/expired session should return to sign-in. Keep infrastructure
      // and server errors visible instead of masking them as an auth failure.
      if (error instanceof ImageHubApiError && error.status === 401) {
        useAuthStore.getState().auth.reset()
        throw redirect({ to: '/sign-in', search: { redirect: location.href } })
      }
      throw error
    }
  },
  component: AuthenticatedLayout,
})
