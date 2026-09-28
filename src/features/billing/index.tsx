import { useCallback, useEffect, useState } from 'react'
import { Check, CreditCard, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { createCheckoutSession, listPlans, listTeams, upsertSubscription, type Plan, type Team } from '@/lib/imagehub-api'

const bytes = (value: number) => value >= 1024 ** 3 ? `${(value / 1024 ** 3).toFixed(0)} GB` : `${(value / 1024 ** 2).toFixed(0)} MB`

export function BillingPage() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [teams, setTeams] = useState<Team[]>([])
  const [loading, setLoading] = useState(true)
  const [savingPlan, setSavingPlan] = useState<string | null>(null)
  const [selectedTeam, setSelectedTeam] = useState('')
  const load = useCallback(() => {
    setLoading(true)
    Promise.all([listPlans(), listTeams()])
      .then(([nextPlans, nextTeams]) => { setPlans(nextPlans); setTeams(nextTeams); setSelectedTeam((current) => current || nextTeams[0]?.id || '') })
      .catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load plans'))
      .finally(() => setLoading(false))
  }, [])
  useEffect(() => { load() }, [load])

  async function choosePlan(planCode: string) {
    const team = teams.find((item) => item.id === selectedTeam) ?? teams[0]
    if (!team) { toast.error('Create a team before choosing a plan'); return }
    setSavingPlan(planCode)
    try {
      const plan = plans.find((item) => item.code === planCode)
      if (plan && plan.price_cents > 0) { const checkout = await createCheckoutSession(team.id, planCode); window.location.assign(checkout.url); return }
      await upsertSubscription(team.id, planCode); toast.success(`Plan updated for ${team.name}`); await load()
    }
    catch (error) { toast.error(error instanceof Error ? error.message : 'Could not update plan') }
    finally { setSavingPlan(null) }
  }

  return <>
    <Header fixed><h1 className='text-lg font-semibold'>Billing</h1></Header>
    <Main className='space-y-6'><div className='flex flex-wrap items-end justify-between gap-3'><div><h1 className='text-2xl font-bold tracking-tight'>Plans & billing</h1><p className='text-muted-foreground'>Review storage, bandwidth and member limits for your account.</p></div><div className='flex gap-2'>{teams.length > 1 && <select className='h-9 rounded-md border bg-background px-3 text-sm' value={selectedTeam} onChange={(event) => setSelectedTeam(event.target.value)}>{teams.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}</select>}<Button variant='outline' onClick={load} disabled={loading}><RefreshCw className='h-4 w-4' />Refresh</Button></div></div>
      {plans.length === 0 ? <Card><CardContent className='flex flex-col items-center gap-3 py-14 text-center text-muted-foreground'><CreditCard className='h-10 w-10' /><p>{loading ? 'Loading plans…' : 'No active plans are configured yet.'}</p></CardContent></Card> : <div className='grid gap-5 md:grid-cols-2 xl:grid-cols-3'>{plans.map((plan) => { const features = Array.isArray(plan.features) ? plan.features : Object.keys(plan.features ?? {}); return <Card key={plan.code} className='flex flex-col'><CardHeader><div className='flex items-center justify-between gap-2'><CardTitle>{plan.name}</CardTitle><Badge variant='outline'>{plan.code}</Badge></div><CardDescription className='text-3xl font-bold text-foreground'>{plan.price_cents === 0 ? 'Free' : `$${(plan.price_cents / 100).toFixed(2)} / month`}</CardDescription></CardHeader><CardContent className='flex flex-1 flex-col gap-4'><div className='space-y-2 text-sm'><p><strong>{bytes(plan.quota_bytes)}</strong> storage</p><p><strong>{bytes(plan.bandwidth_bytes)}</strong> bandwidth</p><p><strong>{plan.member_limit || 'Unlimited'}</strong> team members</p></div>{features.length > 0 && <ul className='space-y-2 border-t pt-4 text-sm'>{features.map((feature) => <li key={feature} className='flex items-center gap-2'><Check className='h-4 w-4 text-primary' />{feature}</li>)}</ul>}<Button className='mt-auto' variant='outline' onClick={() => void choosePlan(plan.code)} disabled={savingPlan !== null}>{savingPlan === plan.code ? 'Updating…' : 'Choose plan'}</Button></CardContent></Card> })}</div>}
    </Main>
  </>
}
