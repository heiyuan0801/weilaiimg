import { useCallback, useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, Copy, FileImage, ImagePlus, Link2, RefreshCw, Search, Trash2, UploadCloud, Video } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { imageHubFetch, listTeams, updateImageVisibility, uploadImage, type Team } from '@/lib/imagehub-api'
import { useI18n } from '@/lib/i18n'

type ImageRecord = { id: string; name: string; mime_type: string; size_bytes: number; url: string; thumbnail_url?: string; created_at: string; status?: string; visibility?: 'private' | 'public' | 'link' }
const PAGE_SIZE = 24
const formatBytes = (bytes: number) => bytes >= 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`

export function ImageLibrary() {
  const { t } = useI18n()
  const [images, setImages] = useState<ImageRecord[]>([])
  const [remoteURL, setRemoteURL] = useState('')
  const [busy, setBusy] = useState(false)
  const [page, setPage] = useState(1)
  const [query, setQuery] = useState('')
  const [mimeFilter, setMimeFilter] = useState('all')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [teams, setTeams] = useState<Team[]>([])
  const [teamId, setTeamId] = useState('')
  const input = useRef<HTMLInputElement>(null)
  const load = useCallback(() => imageHubFetch<ImageRecord[]>('/api/v1/images').then((items) => { setImages(items); setPage(1); setSelectedIds(new Set()) }).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load images')), [])
  useEffect(() => { void load(); void listTeams().then(setTeams).catch(() => undefined) }, [load])
  async function upload(file: File) {
    await uploadImage(file, { teamId: teamId || undefined })
  }
  async function uploadFiles(files: File[]) {
    if (!files.length) return; setBusy(true)
    const results = await Promise.allSettled(files.map(upload))
    const failed = results.filter((result): result is PromiseRejectedResult => result.status === 'rejected')
    if (failed.length) toast.error(`${failed.length} of ${files.length} files failed to upload`)
    if (failed.length < files.length) toast.success(`${files.length - failed.length} file${files.length - failed.length === 1 ? '' : 's'} uploaded`)
    setBusy(false); await load()
  }
  async function importRemote() {
    if (!remoteURL.trim()) return; setBusy(true)
    try { await imageHubFetch('/api/v1/images/import-url', { method: 'POST', body: JSON.stringify({ url: remoteURL.trim(), ...(teamId ? { team_id: teamId } : {}) }) }); setRemoteURL(''); toast.success('Remote file imported'); await load() } catch (error) { toast.error(error instanceof Error ? error.message : 'Import failed') } finally { setBusy(false) }
  }
  function toggleSelected(id: string, checked: boolean) {
    setSelectedIds((current) => {
      const next = new Set(current)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }
  async function remove(id: string) {
    try {
      await imageHubFetch(`/api/v1/images/${id}`, { method: 'DELETE' })
      setImages((current) => current.filter((item) => item.id !== id))
      setSelectedIds((current) => { const next = new Set(current); next.delete(id); return next })
      toast.success('Image moved to trash')
    } catch (error) { toast.error(error instanceof Error ? error.message : 'Delete failed') }
  }
  async function removeSelected() {
    const ids = Array.from(selectedIds)
    if (!ids.length) return
    setBusy(true)
    const results = await Promise.allSettled(ids.map((id) => imageHubFetch(`/api/v1/images/${id}`, { method: 'DELETE' })))
    const failedIds = ids.filter((_, index) => results[index]?.status === 'rejected')
    const deletedIds = ids.filter((_, index) => results[index]?.status === 'fulfilled')
    if (deletedIds.length) {
      setImages((current) => current.filter((item) => !deletedIds.includes(item.id)))
      toast.success(`${deletedIds.length} file${deletedIds.length === 1 ? '' : 's'} moved to trash`)
    }
    if (failedIds.length) toast.error(`${failedIds.length} file${failedIds.length === 1 ? '' : 's'} could not be deleted`)
    setSelectedIds(new Set(failedIds))
    setBusy(false)
  }
  async function copy(value: string) { try { await navigator.clipboard.writeText(value); toast.success('Link copied') } catch { toast.error('Clipboard access is unavailable') } }
  async function copyFormat(image: ImageRecord, format: 'markdown' | 'html' | 'bbcode') {
    const value = format === 'markdown' ? `![${image.name}](${image.url})` : format === 'html' ? `<img src="${image.url}" alt="${image.name}">` : `[img]${image.url}[/img]`
    await copy(value)
  }
  async function setVisibility(image: ImageRecord, visibility: 'private' | 'public' | 'link') {
    try { const result = await updateImageVisibility(image.id, visibility); setImages((current) => current.map((item) => item.id === image.id ? { ...item, visibility: result.visibility as ImageRecord['visibility'], url: result.url } : item)); toast.success('Visibility updated') }
    catch (error) { toast.error(error instanceof Error ? error.message : 'Could not update visibility') }
  }
  const normalizedQuery = query.trim().toLowerCase()
  const filteredImages = images.filter((image) => {
    const matchesQuery = !normalizedQuery || image.name.toLowerCase().includes(normalizedQuery) || image.mime_type.toLowerCase().includes(normalizedQuery)
    const matchesMime = mimeFilter === 'all' || (mimeFilter === 'image' && image.mime_type.startsWith('image/')) || (mimeFilter === 'video' && image.mime_type.startsWith('video/')) || (mimeFilter === 'svg' && image.mime_type === 'image/svg+xml') || (mimeFilter === 'gif' && image.mime_type === 'image/gif')
    return matchesQuery && matchesMime
  })
  const pageCount = Math.max(1, Math.ceil(filteredImages.length / PAGE_SIZE))
  const visibleImages = filteredImages.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
  const filteredIds = filteredImages.map((image) => image.id)
  const allFilteredSelected = filteredIds.length > 0 && filteredIds.every((id) => selectedIds.has(id))
  const someFilteredSelected = filteredIds.some((id) => selectedIds.has(id))
  function toggleAllFiltered(checked: boolean) {
    setSelectedIds((current) => {
      const next = new Set(current)
      filteredIds.forEach((id) => checked ? next.add(id) : next.delete(id))
      return next
    })
  }

  return <>
    <Header fixed><h1 className='text-lg font-semibold'>{t('images.title')}</h1></Header>
    <Main className='space-y-6'>
      <div className='flex flex-wrap items-end justify-between gap-3'><div><h1 className='text-2xl font-bold tracking-tight'>{t('images.title')}</h1><p className='text-muted-foreground'>{t('images.description')}</p></div><div className='flex gap-2'>{teams.length > 0 && <select className='h-9 rounded-md border bg-background px-3 text-sm' value={teamId} onChange={(event) => setTeamId(event.target.value)}><option value=''>Personal library</option>{teams.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}</select>}<Button variant='outline' onClick={load} disabled={busy}><RefreshCw className='me-2 h-4 w-4' />Refresh</Button></div></div>
      <div className='grid gap-6 lg:grid-cols-2'>
        <Card><CardHeader><CardTitle>{t('images.upload')}</CardTitle><CardDescription>{t('images.uploadDescription')}</CardDescription></CardHeader><CardContent><button type='button' disabled={busy} onClick={() => input.current?.click()} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); void uploadFiles(Array.from(event.dataTransfer.files)) }} className='flex w-full flex-col items-center justify-center gap-3 rounded-lg border border-dashed p-10 text-center transition-colors hover:bg-muted'><UploadCloud className='h-8 w-8 text-muted-foreground' /><span className='font-medium'>{busy ? 'Working…' : t('images.choose')}</span><span className='text-sm text-muted-foreground'>JPEG, PNG, WebP, AVIF, GIF, SVG, MP4 and WebM</span></button><input ref={input} type='file' multiple hidden accept='image/*,video/mp4,video/webm' onChange={(event) => { void uploadFiles(Array.from(event.target.files ?? [])); event.currentTarget.value = '' }} /></CardContent></Card>
        <Card><CardHeader><CardTitle>{t('images.remote')}</CardTitle><CardDescription>{t('images.remoteDescription')}</CardDescription></CardHeader><CardContent className='flex gap-2'><Input value={remoteURL} onChange={(event) => setRemoteURL(event.target.value)} placeholder='https://example.com/image.png' onKeyDown={(event) => { if (event.key === 'Enter') void importRemote() }} /><Button disabled={busy || !remoteURL.trim()} onClick={importRemote}><Link2 className='me-2 h-4 w-4' />{t('images.import')}</Button></CardContent></Card>
      </div>
      {images.length === 0 ? <Card><CardContent className='flex flex-col items-center gap-3 py-16 text-center text-muted-foreground'><ImagePlus className='h-10 w-10' /><p>{t('images.noMedia')}</p></CardContent></Card> : <>
        <div className='flex flex-wrap items-center gap-3'>
          <div className='relative min-w-55 flex-1'>
            <Search className='pointer-events-none absolute start-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground' />
            <Input value={query} onChange={(event) => { setQuery(event.target.value); setPage(1) }} placeholder='Search filename or MIME type' className='ps-9' aria-label='Search media' />
          </div>
          <Select value={mimeFilter} onValueChange={(value) => { setMimeFilter(value); setPage(1) }}>
            <SelectTrigger className='w-40'><SelectValue placeholder='Filter type' /></SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>All types</SelectItem>
              <SelectItem value='image'>Images</SelectItem>
              <SelectItem value='video'>Videos</SelectItem>
              <SelectItem value='svg'>SVG</SelectItem>
              <SelectItem value='gif'>GIF</SelectItem>
            </SelectContent>
          </Select>
          <div className='flex items-center gap-2 text-sm text-muted-foreground'>
            <Checkbox checked={allFilteredSelected ? true : someFilteredSelected ? 'indeterminate' : false} onCheckedChange={(value) => toggleAllFiltered(!!value)} aria-label='Select filtered media' disabled={!filteredImages.length || busy} />
            <span>Select filtered ({filteredImages.length})</span>
          </div>
          {selectedIds.size > 0 && <Button variant='destructive' size='sm' onClick={() => void removeSelected()} disabled={busy}><Trash2 className='me-2 h-4 w-4' />Delete selected ({selectedIds.size})</Button>}
        </div>
        {filteredImages.length === 0 ? <Card><CardContent className='flex flex-col items-center gap-3 py-16 text-center text-muted-foreground'><Search className='h-10 w-10' /><p>No media matches the current search or filter.</p><Button variant='outline' size='sm' onClick={() => { setQuery(''); setMimeFilter('all'); setPage(1) }}>Clear filters</Button></CardContent></Card> : <>
          <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4'>{visibleImages.map((image) => { const isVideo = image.mime_type.startsWith('video/'); const status = image.status ?? 'ready'; return <Card key={image.id} className='overflow-hidden'><div className='relative flex aspect-square items-center justify-center bg-muted'>{isVideo ? <video src={image.url} poster={image.thumbnail_url} className='h-full w-full object-contain' controls preload='metadata' /> : image.mime_type.startsWith('image/') ? <img src={image.url} alt={image.name} className='h-full w-full object-contain' loading='lazy' /> : <div className='flex flex-col items-center gap-2 text-center text-sm text-muted-foreground'><FileImage className='h-8 w-8' />{image.mime_type}</div>}<Checkbox checked={selectedIds.has(image.id)} onCheckedChange={(value) => toggleSelected(image.id, !!value)} aria-label={`Select ${image.name}`} disabled={busy} className='absolute start-3 top-3 z-10 bg-background/80' /><Badge variant={status === 'ready' ? 'secondary' : 'outline'} className='absolute end-2 top-2'>{status}</Badge></div><CardContent className='space-y-2 p-3'><div className='truncate font-medium' title={image.name}>{image.name}</div><div className='flex items-center justify-between gap-2 text-xs text-muted-foreground'><Select value={image.visibility ?? 'private'} onValueChange={(value) => void setVisibility(image, value as 'private' | 'public' | 'link')}><SelectTrigger className='h-7 w-24 text-xs'><SelectValue /></SelectTrigger><SelectContent><SelectItem value='private'>Private</SelectItem><SelectItem value='public'>Public</SelectItem><SelectItem value='link'>Link only</SelectItem></SelectContent></Select><span>{formatBytes(image.size_bytes)}</span><div className='flex items-center gap-1'><Button size='icon' variant='ghost' aria-label={`Copy URL for ${image.name}`} onClick={() => void copy(image.url)}><Copy className='h-4 w-4' /></Button><Button size='icon' variant='ghost' aria-label={`Copy Markdown for ${image.name}`} onClick={() => void copyFormat(image, 'markdown')}>M</Button><Button size='icon' variant='ghost' aria-label={`Copy HTML for ${image.name}`} onClick={() => void copyFormat(image, 'html')}>H</Button><Button size='icon' variant='ghost' aria-label={`Delete ${image.name}`} onClick={() => void remove(image.id)} disabled={busy}><Trash2 className='h-4 w-4' /></Button></div></div></CardContent></Card> })}</div>
          <div className='flex items-center justify-between'><p className='text-sm text-muted-foreground'>Page {page} of {pageCount} · {filteredImages.length} of {images.length} files</p><div className='flex gap-2'><Button variant='outline' size='sm' disabled={page <= 1} onClick={() => setPage((current) => current - 1)}><ChevronLeft className='h-4 w-4' />Previous</Button><Button variant='outline' size='sm' disabled={page >= pageCount} onClick={() => setPage((current) => current + 1)}>Next<ChevronRight className='h-4 w-4' /></Button></div></div>
        </>}
      </>}
    </Main>
  </>
}
