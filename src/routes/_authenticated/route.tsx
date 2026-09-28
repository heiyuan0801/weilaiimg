import { createFileRoute, redirect } from '@tanstack/react-router'
import { AuthenticatedLayout } from '@/components/layout/authenticated-layout'
import { imageHubFetch, ImageHubApiError } from '@/lib/imagehub-api'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ location }) => {
    try { await imageHubFetch('/api/v1/auth/me') }
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
