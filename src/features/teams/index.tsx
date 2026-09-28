import { useCallback, useEffect, useState } from 'react'
import { Mail, Plus, RefreshCw, UserPlus, UsersRound } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { createTeam, createTeamInvitation, listTeamInvitations, listTeamMembers, listTeams, type Team, type TeamInvitation, type TeamMember } from '@/lib/imagehub-api'

const bytes = (value: number) => {
  if (!value) return 'Unlimited'
  if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GB`
  return `${(value / 1024 ** 2).toFixed(0)} MB`
}

export function TeamsPage() {
  const [teams, setTeams] = useState<Team[]>([])
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [saving, setSaving] = useState(false)
  const [selectedTeamId, setSelectedTeamId] = useState<string>()
  const [members, setMembers] = useState<TeamMember[]>([])
  const [invitations, setInvitations] = useState<TeamInvitation[]>([])
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('member')
  const [inviting, setInviting] = useState(false)

  const load = useCallback(() => listTeams().then((items) => { setTeams(items); setSelectedTeamId((current) => current ?? items[0]?.id) }).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load teams')), [])
  useEffect(() => { void load() }, [load])

  useEffect(() => {
    if (!selectedTeamId) return
    Promise.allSettled([listTeamMembers(selectedTeamId), listTeamInvitations(selectedTeamId)]).then(([memberResult, invitationResult]) => { if (memberResult.status === 'fulfilled') setMembers(memberResult.value); else toast.error(memberResult.reason instanceof Error ? memberResult.reason.message : 'Could not load members'); if (invitationResult.status === 'fulfilled') setInvitations(invitationResult.value); else setInvitations([]) })
  }, [selectedTeamId])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!name.trim()) return
    setSaving(true)
    try {
      await createTeam({ name: name.trim(), slug: slug.trim() || undefined })
      setName(''); setSlug(''); toast.success('Team created'); await load()
    } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not create team') } finally { setSaving(false) }
  }

  async function invite(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selectedTeamId || !inviteEmail.trim()) return
    setInviting(true)
    try {
      const invitation = await createTeamInvitation(selectedTeamId, { email: inviteEmail.trim(), role: inviteRole })
      setInvitations((current) => [invitation, ...current]); setInviteEmail(''); toast.success(invitation.status === 'pending' ? `Invitation created for ${invitation.email}` : `Invitation sent to ${invitation.email}`)
    } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not send invitation') } finally { setInviting(false) }
  }

  return <>
    <Header fixed><h1 className='text-lg font-semibold'>Teams</h1></Header>
    <Main className='space-y-6'>
      <div className='flex flex-wrap items-end justify-between gap-3'><div><h1 className='text-2xl font-bold tracking-tight'>Teams</h1><p className='text-muted-foreground'>Share a library, quota and billing plan with your collaborators.</p></div><Button variant='outline' onClick={load}><RefreshCw className='h-4 w-4' />Refresh</Button></div>
      <Card><CardHeader><CardTitle>Create a team</CardTitle><CardDescription>Team owners can invite members and assign a plan from the billing page.</CardDescription></CardHeader><CardContent><form onSubmit={submit} className='grid gap-4 sm:grid-cols-[1fr_1fr_auto] sm:items-end'><div className='space-y-2'><Label htmlFor='team-name'>Name</Label><Input id='team-name' value={name} onChange={(event) => setName(event.target.value)} placeholder='Design team' /></div><div className='space-y-2'><Label htmlFor='team-slug'>Slug (optional)</Label><Input id='team-slug' value={slug} onChange={(event) => setSlug(event.target.value)} placeholder='design-team' /></div><Button type='submit' disabled={saving || !name.trim()}><Plus className='h-4 w-4' />{saving ? 'Creating…' : 'Create team'}</Button></form></CardContent></Card>
      {teams.length === 0 ? <Card><CardContent className='flex flex-col items-center gap-3 py-14 text-center text-muted-foreground'><UsersRound className='h-10 w-10' /><p>No teams yet. Create one to start collaborating.</p></CardContent></Card> : <>
        <div className='grid gap-4 md:grid-cols-2'>{teams.map((team) => { const percent = team.quota_bytes > 0 ? Math.min(100, (team.used_bytes / team.quota_bytes) * 100) : 0; const selected = team.id === selectedTeamId; return <Card key={team.id} className={selected ? 'border-primary' : undefined}><CardHeader><div className='flex items-start justify-between gap-3'><div><CardTitle>{team.name}</CardTitle><CardDescription>{team.slug} · {team.role}</CardDescription></div><span className='rounded-full bg-muted px-2 py-1 text-xs'>{team.plan_code}</span></div></CardHeader><CardContent className='space-y-3'><div className='flex justify-between text-sm'><span>Storage</span><span>{bytes(team.used_bytes)} / {bytes(team.quota_bytes)}</span></div><div className='h-2 overflow-hidden rounded-full bg-muted'><div className='h-full rounded-full bg-primary transition-all' style={{ width: `${percent}%` }} /></div><Button variant={selected ? 'secondary' : 'outline'} size='sm' onClick={() => setSelectedTeamId(team.id)}><UsersRound className='h-4 w-4' />Manage members</Button></CardContent></Card> })}</div>
        <div className='grid gap-6 lg:grid-cols-2'>
          <Card><CardHeader><CardTitle className='flex items-center gap-2'><UserPlus className='h-5 w-5' />Invite a member</CardTitle><CardDescription>Invitations expire after seven days and can be accepted once.</CardDescription></CardHeader><CardContent><form onSubmit={invite} className='space-y-4'><div className='space-y-2'><Label htmlFor='invite-email'>Email address</Label><Input id='invite-email' type='email' required value={inviteEmail} onChange={(event) => setInviteEmail(event.target.value)} placeholder='teammate@example.com' /></div><div className='space-y-2'><Label htmlFor='invite-role'>Role</Label><select id='invite-role' value={inviteRole} onChange={(event) => setInviteRole(event.target.value)} className='h-9 w-full rounded-md border bg-transparent px-3 text-sm'><option value='member'>Member</option><option value='admin'>Admin</option><option value='billing'>Billing</option></select></div><Button type='submit' disabled={inviting || !selectedTeamId || !inviteEmail.trim()}><Mail className='h-4 w-4' />{inviting ? 'Sending…' : 'Send invitation'}</Button></form></CardContent></Card>
          <Card><CardHeader><CardTitle>Members ({members.length})</CardTitle><CardDescription>People with access to the selected team.</CardDescription></CardHeader><CardContent>{members.length === 0 ? <p className='py-6 text-sm text-muted-foreground'>No members found.</p> : <div className='space-y-3'>{members.map((member) => <div key={member.user_id} className='flex items-center justify-between gap-3 rounded-md border p-3'><div><p className='font-medium'>{member.display_name || member.email}</p>{member.display_name && <p className='text-sm text-muted-foreground'>{member.email}</p>}</div><span className='rounded-full bg-muted px-2 py-1 text-xs capitalize'>{member.role}</span></div>)}</div>}</CardContent></Card>
        </div>
        <Card><CardHeader><CardTitle>Pending invitations ({invitations.length})</CardTitle><CardDescription>Track delivery and acceptance for this team.</CardDescription></CardHeader><CardContent>{invitations.length === 0 ? <p className='py-4 text-sm text-muted-foreground'>No pending invitations.</p> : <div className='space-y-3'>{invitations.map((invitation) => <div key={invitation.id} className='flex flex-wrap items-center justify-between gap-3 rounded-md border p-3'><div className='flex items-center gap-2'><Mail className='h-4 w-4 text-muted-foreground' /><span>{invitation.email}</span></div><div className='flex items-center gap-2 text-sm text-muted-foreground'><span className='capitalize'>{invitation.role}</span><span>·</span><span className='capitalize'>{invitation.status ?? (invitation.accepted_at ? 'accepted' : 'pending')}</span><span>· expires {new Date(invitation.expires_at).toLocaleDateString()}</span></div></div>)}</div>}</CardContent></Card>
      </>}
    </Main>
  </>
}
