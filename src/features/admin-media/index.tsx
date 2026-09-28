import { useCallback, useEffect, useState } from 'react'
import { Images, List, RefreshCw, Search, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { imageHubFetch } from '@/lib/imagehub-api'
import { useI18n } from '@/lib/i18n'

type AdminMedia = { id: string; name: string; content_hash: string; mime_type: string; size_bytes: number; width: number; height: number; storage_backend: string; owner_email: string; references: number; created_at: string; url: string; thumbnail_url?: string }
const bytes = (value: number) => value >= 1024 * 1024 ? `${(value / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(value / 1024))} KB`

export function AdminMediaPage() {
  const { t } = useI18n()
  const [items, setItems] = useState<AdminMedia[]>([])
  const [query, setQuery] = useState('')
  const [mime, setMime] = useState('')
  const [loading, setLoading] = useState(false)
  const [viewMode, setViewMode] = useState<'list' | 'grid'>('list')
  const load = useCallback(() => { setLoading(true); void imageHubFetch<AdminMedia[]>(`/api/v1/admin/images?q=${encodeURIComponent(query)}&mime=${encodeURIComponent(mime)}`).then(setItems).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load media')).finally(() => setLoading(false)) }, [mime, query])
  useEffect(() => { load() }, [load])
  async function forceDelete(item: AdminMedia) {
    if (!window.confirm(t('adminMedia.deleteConfirm'))) return
    try { await imageHubFetch(`/api/v1/admin/images/${item.id}`, { method: 'DELETE' }); setItems((current) => current.filter((entry) => entry.id !== item.id)); toast.success(t('adminMedia.deleted')) }
    catch (error) { toast.error(error instanceof Error ? error.message : 'Could not delete media') }
  }
  const preview = (item: AdminMedia, className: string) => item.mime_type.startsWith('video/')
    ? <video className={className} src={item.url} controls muted preload='metadata' aria-label={item.name} />
    : item.thumbnail_url || item.mime_type.startsWith('image/')
      ? <img src={item.thumbnail_url || item.url} alt={item.name} className={className} loading='lazy' />
      : <div className={`${className} flex items-center justify-center text-xs text-muted-foreground`}>{item.mime_type}</div>

  return <>
    <Header fixed><h1 className='text-lg font-semibold'>{t('adminMedia.title')}</h1></Header>
    <Main className='space-y-6'>
      <div className='flex flex-wrap items-end justify-between gap-3'>
        <div><h1 className='text-2xl font-bold tracking-tight'>{t('adminMedia.title')}</h1><p className='text-muted-foreground'>{t('adminMedia.description')}</p></div>
        <div className='flex gap-2'>
          <div className='flex rounded-md border p-1' role='group' aria-label={t('adminMedia.viewMode')}>
            <Button size='sm' variant={viewMode === 'list' ? 'secondary' : 'ghost'} aria-pressed={viewMode === 'list'} onClick={() => setViewMode('list')}><List className='me-2 h-4 w-4' />{t('adminMedia.listView')}</Button>
            <Button size='sm' variant={viewMode === 'grid' ? 'secondary' : 'ghost'} aria-pressed={viewMode === 'grid'} onClick={() => setViewMode('grid')}><Images className='me-2 h-4 w-4' />{t('adminMedia.gridView')}</Button>
          </div>
          <Button variant='outline' onClick={load} disabled={loading}><RefreshCw className='me-2 h-4 w-4' />{t('adminMedia.refresh')}</Button>
        </div>
      </div>
      <Card><CardHeader><CardTitle>{t('adminMedia.inventory')}</CardTitle><CardDescription>{t('adminMedia.inventoryDescription')}</CardDescription></CardHeader><CardContent className='space-y-4'>
        <div className='flex flex-wrap gap-2'><div className='relative min-w-64 flex-1'><Search className='pointer-events-none absolute start-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground' /><Input className='ps-9' placeholder={t('adminMedia.searchPlaceholder')} value={query} onChange={(event) => setQuery(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') load() }} /></div><Input className='w-52' placeholder={t('adminMedia.mimePlaceholder')} value={mime} onChange={(event) => setMime(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') load() }} /><Button onClick={load} disabled={loading}>{t('adminMedia.search')}</Button></div>
        {viewMode === 'list' ? <div className='overflow-x-auto'><table className='w-full text-sm'><thead><tr className='border-b text-left'><th className='p-2'>{t('adminMedia.preview')}</th><th className='p-2'>{t('adminMedia.file')}</th><th className='p-2'>{t('adminMedia.owner')}</th><th className='p-2'>{t('adminMedia.typeSize')}</th><th className='p-2'>{t('adminMedia.storageRefs')}</th><th className='p-2'>{t('adminMedia.created')}</th><th className='p-2 text-right'>{t('adminMedia.action')}</th></tr></thead><tbody>{items.map((item) => <tr key={item.id} className='border-b align-middle'><td className='p-2'>{preview(item, 'h-12 w-12 rounded bg-muted object-contain')}</td><td className='max-w-64 p-2'><div className='truncate font-medium' title={item.name}>{item.name}</div><code className='text-xs text-muted-foreground'>{item.content_hash.slice(0, 16)}…</code></td><td className='p-2'>{item.owner_email || <Badge variant='outline'>{t('adminMedia.guest')}</Badge>}</td><td className='p-2'>{item.mime_type}<br /><span className='text-muted-foreground'>{bytes(item.size_bytes)}{item.width > 0 ? ` · ${item.width}×${item.height}` : ''}</span></td><td className='p-2'>{item.storage_backend}<br /><span className='text-muted-foreground'>{item.references} refs</span></td><td className='whitespace-nowrap p-2'>{new Date(item.created_at).toLocaleString()}</td><td className='p-2 text-right'><Button size='icon' variant='destructive' aria-label={`Force delete ${item.name}`} onClick={() => void forceDelete(item)}><Trash2 className='h-4 w-4' /></Button></td></tr>)}</tbody></table></div> : <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>{items.map((item) => <Card key={item.id} className='overflow-hidden'><div className='flex aspect-square items-center justify-center bg-muted'>{preview(item, 'h-full w-full object-contain')}</div><CardContent className='space-y-2 p-3'><div className='truncate font-medium' title={item.name}>{item.name}</div><div className='text-xs text-muted-foreground'>{item.mime_type} · {bytes(item.size_bytes)}</div><div className='flex items-center justify-between gap-2'><span className='truncate text-xs text-muted-foreground'>{item.owner_email || t('adminMedia.guest')}</span><Button size='icon' variant='destructive' aria-label={`Force delete ${item.name}`} onClick={() => void forceDelete(item)}><Trash2 className='h-4 w-4' /></Button></div></CardContent></Card>)}</div>}
        {!loading && !items.length && <p className='py-12 text-center text-muted-foreground'>{t('adminMedia.noResults')}</p>}
      </CardContent></Card>
    </Main>
  </>
}
