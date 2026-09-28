import { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, Copy, Globe2, Plus, RefreshCw } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { createDomain, deleteDomain, listDomains, verifyDomain, type CustomDomain } from '@/lib/imagehub-api'

export function DomainsPage() {
  const [domains, setDomains] = useState<CustomDomain[]>([])
  const [domain, setDomain] = useState('')
  const [verification, setVerification] = useState<CustomDomain['verification']>()
  const [saving, setSaving] = useState(false)
  const [verifying, setVerifying] = useState<string | null>(null)
  const load = useCallback(() => listDomains().then(setDomains).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load domains')), [])
  useEffect(() => { void load() }, [load])

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!domain.trim()) return; setSaving(true)
    try { const result = await createDomain(domain.trim()); setDomains((current) => [result, ...current]); setVerification(result.verification); setDomain(''); toast.success('Domain added; publish the verification record to continue') } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not add domain') } finally { setSaving(false) }
  }
  async function copy(value: string) { try { await navigator.clipboard.writeText(value); toast.success('Copied to clipboard') } catch { toast.error('Clipboard access is unavailable') } }
  async function verify(id: string) {
    setVerifying(id)
    try { await verifyDomain(id); toast.success('Domain verified; TLS can now be provisioned by your proxy or CDN'); await load() }
    catch (error) { toast.error(error instanceof Error ? error.message : 'Domain verification failed') }
    finally { setVerifying(null) }
  }
  async function remove(id: string) { try { await deleteDomain(id); setDomains((current) => current.filter((item) => item.id !== id)); toast.success('Domain removed') } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not remove domain') } }

  return <>
    <Header fixed><h1 className='text-lg font-semibold'>Custom domains</h1></Header>
    <Main className='space-y-6'><div className='flex flex-wrap items-end justify-between gap-3'><div><h1 className='text-2xl font-bold tracking-tight'>Custom domains</h1><p className='text-muted-foreground'>Serve your media from a branded domain with DNS verification and managed TLS.</p></div><Button variant='outline' onClick={load}><RefreshCw className='h-4 w-4' />Refresh</Button></div>
      <Card><CardHeader><CardTitle>Add a domain</CardTitle><CardDescription>We will provide a TXT record and check it before enabling the domain.</CardDescription></CardHeader><CardContent><form onSubmit={submit} className='flex flex-col gap-3 sm:flex-row sm:items-end'><div className='flex-1 space-y-2'><Label htmlFor='custom-domain'>Domain</Label><Input id='custom-domain' value={domain} onChange={(event) => setDomain(event.target.value)} placeholder='img.example.com' /></div><Button type='submit' disabled={saving || !domain.trim()}><Plus className='h-4 w-4' />{saving ? 'Adding…' : 'Add domain'}</Button></form></CardContent></Card>
      {verification && <Card className='border-primary/40'><CardHeader><CardTitle className='flex items-center gap-2'><CheckCircle2 className='h-5 w-5 text-primary' />DNS verification required</CardTitle><CardDescription>Create this TXT record at your DNS provider, then refresh this page.</CardDescription></CardHeader><CardContent className='grid gap-3 text-sm sm:grid-cols-3'><div><span className='text-muted-foreground'>Type</span><p className='font-mono'>{verification.type}</p></div><div><span className='text-muted-foreground'>Name</span><div className='flex items-center gap-2'><p className='truncate font-mono'>{verification.name}</p><Button size='icon' variant='ghost' onClick={() => void copy(verification.name)}><Copy className='h-4 w-4' /></Button></div></div><div><span className='text-muted-foreground'>Value</span><div className='flex items-center gap-2'><p className='truncate font-mono'>{verification.value}</p><Button size='icon' variant='ghost' onClick={() => void copy(verification.value)}><Copy className='h-4 w-4' /></Button></div></div></CardContent></Card>}
      <Card><CardHeader><CardTitle>Configured domains</CardTitle></CardHeader><CardContent>{domains.length === 0 ? <div className='flex flex-col items-center gap-3 py-12 text-center text-muted-foreground'><Globe2 className='h-10 w-10' /><p>No custom domains configured.</p></div> : <Table><TableHeader><TableRow><TableHead>Domain</TableHead><TableHead>Status</TableHead><TableHead>TLS</TableHead><TableHead>Default</TableHead><TableHead className='text-right'>Action</TableHead></TableRow></TableHeader><TableBody>{domains.map((item) => <TableRow key={item.id}><TableCell className='font-medium'>{item.domain}</TableCell><TableCell><Badge variant={item.verified_at ? 'default' : 'outline'}>{item.verified_at ? 'Verified' : 'Pending DNS'}</Badge></TableCell><TableCell>{item.tls_status}</TableCell><TableCell>{item.is_default ? 'Yes' : 'No'}</TableCell><TableCell className='flex justify-end gap-2 text-right'>{item.verified_at ? '—' : <Button size='sm' variant='outline' onClick={() => void verify(item.id)} disabled={verifying !== null}>{verifying === item.id ? 'Checking…' : 'Verify DNS'}</Button>}<Button size='sm' variant='ghost' onClick={() => void remove(item.id)}>Remove</Button></TableCell></TableRow>)}</TableBody></Table>}</CardContent></Card>
    </Main>
  </>
}
