import { useCallback, useEffect, useRef, useState } from 'react'
import { Check, Copy, Database, ExternalLink, ImagePlus, Languages, LogIn, UploadCloud, Users } from 'lucide-react'
import { Link } from '@tanstack/react-router'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { imageHubFetch, uploadGuestImage, type GuestUploadResult } from '@/lib/imagehub-api'
import { useI18n } from '@/lib/i18n'

type PublicConfig = {
  public_url: string
  registration?: { enabled?: boolean }
  upload?: { max_file_bytes?: number; anonymous_enabled?: boolean; guest_retention_days?: number }
}

const formatBytes = (bytes: number) => bytes >= 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`
function formats(result: GuestUploadResult) {
  const name = result.name || 'image'
  return {
    URL: result.url,
    'Short URL': result.short_url || result.url,
    Markdown: `![${name}](${result.url})`,
    HTML: `<img src="${result.url}" alt="${name}">`,
    BBCode: `[img]${result.url}[/img]`,
    'Markdown link': `[${name}](${result.url})`,
  }
}

export function PublicHome() {
  const { locale, setLocale, t } = useI18n()
  const [config, setConfig] = useState<PublicConfig | null>(null)
  const [files, setFiles] = useState<GuestUploadResult[]>([])
  const [progress, setProgress] = useState<Record<string, number>>({})
  const [busy, setBusy] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const copyValue = async (value: string) => {
    try { await navigator.clipboard.writeText(value); toast.success(t('upload.copied')) }
    catch { toast.error(t('upload.clipboardUnavailable')) }
  }

  useEffect(() => { void imageHubFetch<PublicConfig>('/api/v1/public/config').then(setConfig).catch(() => undefined) }, [])

  const uploadFiles = useCallback(async (items: File[]) => {
    if (!items.length) return
    setBusy(true)
    const results: GuestUploadResult[] = []
    for (const file of items) {
      const key = `${file.name}:${file.size}:${file.lastModified}`
      try {
        const result = await uploadGuestImage(file, (value) => setProgress((current) => ({ ...current, [key]: value })))
        results.push(result)
      } catch (error) {
        toast.error(error instanceof Error ? error.message : `Could not upload ${file.name}`)
      }
    }
    if (results.length) {
      setFiles((current) => [...results, ...current])
      toast.success(`${results.length} file${results.length === 1 ? '' : 's'} uploaded`)
    }
    setBusy(false)
  }, [])

  useEffect(() => {
    const onPaste = (event: ClipboardEvent) => {
      const pasted = Array.from(event.clipboardData?.files ?? []).filter((file) => file.type.startsWith('image/'))
      if (pasted.length) { event.preventDefault(); void uploadFiles(pasted) }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  }, [uploadFiles])

  const guestEnabled = config?.upload?.anonymous_enabled !== false
  return <div className='min-h-svh bg-muted/20'>
    <header className='border-b bg-background/90 backdrop-blur'>
      <div className='mx-auto flex h-16 max-w-6xl items-center justify-between px-4'>
        <Link to='/' className='flex items-center gap-2 font-semibold'><ImagePlus className='h-5 w-5 text-primary' />{t('brand.name')}</Link>
        <nav className='flex items-center gap-2'>
          <Button variant='ghost' size='sm' asChild><Link to='/sign-in'><LogIn className='me-2 h-4 w-4' />{t('nav.signIn')}</Link></Button>
          {config?.registration?.enabled !== false && <Button size='sm' asChild><Link to='/sign-up'>{t('nav.register')}</Link></Button>}
          <Button variant='ghost' size='sm' onClick={() => setLocale(locale === 'zh-CN' ? 'en-US' : 'zh-CN')} aria-label='Change language' title='中文 / English'><Languages className='me-2 h-4 w-4' />{t('nav.language')}</Button>
        </nav>
      </div>
    </header>
    <main className='mx-auto flex max-w-5xl flex-col gap-8 px-4 py-12'>
      <section className='space-y-3 text-center'>
        <Badge variant='secondary'>{t('home.badge')}</Badge>
        <h1 className='text-4xl font-bold tracking-tight sm:text-5xl'>{t('home.title')}</h1>
        <p className='mx-auto max-w-2xl text-muted-foreground'>{t('home.subtitle')}</p>
        <div className='flex flex-wrap justify-center gap-3 pt-2'>
          {config?.registration?.enabled !== false && <Button asChild><Link to='/sign-up'>{t('home.registerAction')}</Link></Button>}
          <Button variant='outline' asChild><Link to='/sign-in'><LogIn className='me-2 h-4 w-4' />{t('home.loginAction')}</Link></Button>
        </div>
      </section>
      <section className='grid gap-4 md:grid-cols-3'>
        <Card><CardContent className='flex gap-3 p-5'><UploadCloud className='mt-1 h-5 w-5 text-primary' /><div><h2 className='font-semibold'>{t('home.featureMediaTitle')}</h2><p className='text-sm text-muted-foreground'>{t('home.featureMediaDescription')}</p></div></CardContent></Card>
        <Card><CardContent className='flex gap-3 p-5'><Database className='mt-1 h-5 w-5 text-primary' /><div><h2 className='font-semibold'>{t('home.featureStorageTitle')}</h2><p className='text-sm text-muted-foreground'>{t('home.featureStorageDescription')}</p></div></CardContent></Card>
        <Card><CardContent className='flex gap-3 p-5'><Users className='mt-1 h-5 w-5 text-primary' /><div><h2 className='font-semibold'>{t('home.featureTeamTitle')}</h2><p className='text-sm text-muted-foreground'>{t('home.featureTeamDescription')}</p></div></CardContent></Card>
      </section>
      <Card>
        <CardHeader><CardTitle>{t('upload.title')}</CardTitle><CardDescription>{guestEnabled ? `${t('upload.guestEnabled')}${config?.upload?.guest_retention_days ? ` · ${t('upload.expiresAfter')} ${config.upload.guest_retention_days} days.` : '.'}` : t('upload.disabled')}</CardDescription></CardHeader>
        <CardContent>
          <button type='button' disabled={busy || !guestEnabled} onClick={() => input.current?.click()} onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); void uploadFiles(Array.from(event.dataTransfer.files)) }} className='flex min-h-64 w-full flex-col items-center justify-center gap-4 rounded-xl border-2 border-dashed p-8 text-center transition-colors hover:bg-muted disabled:cursor-not-allowed disabled:opacity-60'>
            <UploadCloud className='h-12 w-12 text-primary' />
            <span className='text-lg font-medium'>{t('upload.drop')}</span>
            <span className='text-sm text-muted-foreground'>{t('upload.paste')}</span>
            <span className='text-xs text-muted-foreground'>JPEG, PNG, WebP, AVIF, GIF, SVG, MP4, WebM · max {formatBytes(config?.upload?.max_file_bytes ?? 20 * 1024 * 1024)}</span>
          </button>
          <input ref={input} type='file' multiple hidden accept='image/*,video/mp4,video/webm' onChange={(event) => { void uploadFiles(Array.from(event.target.files ?? [])); event.currentTarget.value = '' }} />
          {Object.entries(progress).some(([, value]) => value > 0 && value < 100) && <div className='mt-4 space-y-2'>{Object.entries(progress).filter(([, value]) => value > 0 && value < 100).map(([key, value]) => <div key={key} className='space-y-1'><div className='flex justify-between text-xs text-muted-foreground'><span className='truncate'>{key.split(':')[0]}</span><span>{value}%</span></div><Progress value={value} /></div>)}</div>}
        </CardContent>
      </Card>
      {files.length > 0 && <section className='space-y-4'><div className='flex items-center justify-between'><h2 className='text-xl font-semibold'>{t('upload.results')}</h2><Button variant='outline' size='sm' onClick={() => void copyValue(files.map((file) => file.url).join('\n'))}><Copy className='me-2 h-4 w-4' />{t('upload.copyAll')}</Button></div>{files.map((file) => <Card key={`${file.id}-${file.url}`}><CardContent className='grid gap-5 p-5 md:grid-cols-[180px_1fr]'>{file.mime_type?.startsWith('image/') ? <img src={file.url} alt={file.name} className='aspect-square w-full rounded-lg bg-muted object-contain' /> : <div className='flex aspect-square items-center justify-center rounded-lg bg-muted text-sm text-muted-foreground'>{t('upload.video')}</div>}<div className='min-w-0 space-y-3'><div><div className='truncate font-medium'>{file.name}</div><div className='text-sm text-muted-foreground'>{formatBytes(file.size_bytes)}{file.metadata?.width ? ` · ${file.metadata.width} × ${file.metadata.height}` : ''}</div></div>{Object.entries(formats(file)).map(([label, value]) => <div key={label} className='flex items-center gap-2'><code className='min-w-0 flex-1 overflow-x-auto rounded bg-muted px-2 py-1 text-xs'>{value}</code><Button variant='outline' size='icon' aria-label={`${t('upload.copy')} ${label}`} onClick={() => void copyValue(value)}><Copy className='h-4 w-4' /></Button></div>)}<Button variant='ghost' size='sm' asChild><a href={file.url} target='_blank' rel='noreferrer'><ExternalLink className='me-2 h-4 w-4' />{t('upload.openFile')}</a></Button><div className='flex items-center gap-2 text-xs text-emerald-600'><Check className='h-3 w-3' />{t('upload.ready')}</div></div></CardContent></Card>)}</section>}
    </main>
  </div>
}
