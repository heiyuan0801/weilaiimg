import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/clerk/(auth)/sign-in')({
  beforeLoad: () => { throw redirect({ to: '/sign-in' }) },
})
