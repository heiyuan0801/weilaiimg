import { useEffect, useState } from 'react'
import { Link, createFileRoute, useNavigate, useParams } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { AuthLayout } from '@/features/auth/auth-layout'
import { acceptTeamInvitation } from '@/lib/imagehub-api'

export const Route = createFileRoute('/team-invitations/$token')({ component: TeamInvitationRoute })
function TeamInvitationRoute() {
  const { token } = useParams({ from: '/team-invitations/$token' }); const navigate = useNavigate(); const [state, setState] = useState('Accepting invitation…')
  useEffect(() => { void acceptTeamInvitation(token).then((result) => { setState('Invitation accepted.'); toast.success('You joined the team.'); setTimeout(() => navigate({ to: '/teams' }), 800) }).catch((error) => { setState(error instanceof Error ? error.message : 'Sign in to accept this invitation.'); }) }, [navigate, token])
  return <AuthLayout><div className='mx-auto max-w-sm space-y-4 text-center'><h1 className='text-xl font-semibold'>Team invitation</h1><p className='text-muted-foreground'>{state}</p><Button asChild><Link to='/sign-in' search={{ redirect: `/team-invitations/${token}` }}>Sign in</Link></Button></div></AuthLayout>
}
