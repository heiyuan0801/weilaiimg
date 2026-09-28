import { useEffect, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { createOIDCProvider, deleteOIDCProvider, getSystemSettings, listOIDCProviders, saveSystemSetting, sendTestEmail, type OIDCProvider, type SystemSettings } from '@/lib/imagehub-api'

type ToggleProps = { label: string; checked: boolean; onChange: (value: boolean) => void; description: string }
function Toggle({ label, checked, onChange, description }: ToggleProps) {
  return (
    <div className='flex items-center justify-between gap-4 rounded-lg border p-4'>
      <div>
        <Label>{label}</Label>
        <p className='text-sm text-muted-foreground'>{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  )
}

const defaults: SystemSettings = {
  site: { default_language: 'en-US' },
  registration: { enabled: true, require_email_verification: false, password_reset_enabled: true },
  upload: { max_file_bytes: 20 * 1024 * 1024, daily_upload_limit: 100, allow_svg: true, allow_video: true, allow_remote_url: true, anonymous_enabled: false, guest_daily_upload_limit: 10, guest_daily_upload_bytes: 0, guest_retention_days: 7, short_links_enabled: true, short_code_length: 8, naming_mode: 'sha256', directory_rule: 'hash2', path_template: '', random_length: 12, default_visibility: 'private' },
  email: { smtp_host: '', smtp_port: 587, smtp_username: '', smtp_security: 'starttls', from_name: 'ImageHub', from_address: '' },
  storage: { backend: 'local', telegram_chat_id: '', cdn_base_url: '', s3_endpoint: '', s3_region: 'us-east-1', s3_bucket: '', s3_access_key: '', s3_secret_key: '', s3_prefix: '', s3_path_style: false },
  oidc: { enabled: false, issuer_url: '', client_id: '', scopes: 'openid profile email', auto_create_users: true },
}

export function SystemSettingsPage() {
  const [settings, setSettings] = useState<SystemSettings>(defaults)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState<string | null>(null)
  const [providers, setProviders] = useState<OIDCProvider[]>([])
  const [provider, setProvider] = useState({ name: '', issuer_url: '', client_id: '', client_secret: '' })
  const [testRecipient, setTestRecipient] = useState('')

  useEffect(() => {
    Promise.all([getSystemSettings(), listOIDCProviders()]).then(([data, nextProviders]) => { setSettings({ ...defaults, ...data }); setProviders(nextProviders) }).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not load settings')).finally(() => setLoading(false))
  }, [])

  const update = (group: string, key: string, value: unknown) => setSettings((current) => ({ ...current, [group]: { ...(current[group] ?? {}), [key]: value } }))
  const save = async (group: string) => {
    setSaving(group)
    try {
      await saveSystemSetting(group, settings[group] ?? {})
      toast.success(`${group} settings saved`)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Save failed')
    } finally {
      setSaving(null)
    }
  }
  const value = (group: string, key: string) => settings[group]?.[key] ?? defaults[group]?.[key] ?? ''
  async function addProvider() { try { const created = await createOIDCProvider({ ...provider, scopes: ['openid', 'profile', 'email'], enabled: true, auto_create_users: true }); setProviders((current) => [...current, created]); setProvider({ name: '', issuer_url: '', client_id: '', client_secret: '' }); toast.success('OIDC provider added') } catch (error) { toast.error(error instanceof Error ? error.message : 'Could not add OIDC provider') } }

  if (loading) return <><Header fixed><h1 className='text-lg font-semibold'>System settings</h1></Header><Main><p className='text-muted-foreground'>Loading settings…</p></Main></>

  return (
    <>
      <Header fixed><h1 className='text-lg font-semibold'>System settings</h1></Header>
      <Main className='space-y-6'>
        <div><h1 className='text-2xl font-bold tracking-tight'>System settings</h1><p className='text-muted-foreground'>Configure registration, email delivery, upload policies, storage, CDN and external login.</p></div>
        <div className='grid gap-6 xl:grid-cols-2'>
          <Card>
            <CardHeader><CardTitle>Site language</CardTitle><CardDescription>Visitors use their browser language unless they have selected a language manually.</CardDescription></CardHeader>
            <CardContent className='space-y-4'><div className='space-y-2'><Label>Default language</Label><select className='h-9 w-full rounded-md border bg-transparent px-3 text-sm' value={String(value('site', 'default_language'))} onChange={(event) => update('site', 'default_language', event.target.value)}><option value='en-US'>English</option><option value='zh-CN'>简体中文</option></select></div><Button onClick={() => save('site')} disabled={saving === 'site'}>{saving === 'site' ? 'Saving…' : 'Save site language'}</Button></CardContent>
          </Card>
          <Card>
            <CardHeader><CardTitle>Registration & account</CardTitle><CardDescription>Control how users create and recover accounts.</CardDescription></CardHeader>
            <CardContent className='space-y-4'>
              <Toggle label='Allow user registration' description='New users can create local accounts.' checked={Boolean(value('registration', 'enabled'))} onChange={(v) => update('registration', 'enabled', v)} />
              <Toggle label='Require email verification' description='Keep new accounts pending until the verification link is used.' checked={Boolean(value('registration', 'require_email_verification'))} onChange={(v) => update('registration', 'require_email_verification', v)} />
              <Toggle label='Allow password recovery' description='Send reset links through the configured SMTP service.' checked={Boolean(value('registration', 'password_reset_enabled'))} onChange={(v) => update('registration', 'password_reset_enabled', v)} />
              <Button onClick={() => save('registration')} disabled={saving === 'registration'}>{saving === 'registration' ? 'Saving…' : 'Save registration settings'}</Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Upload policy</CardTitle><CardDescription>Set global limits. User and team overrides are stored on their records.</CardDescription></CardHeader>
            <CardContent className='space-y-4'>
              <div className='grid gap-4 sm:grid-cols-2'><div className='space-y-2'><Label>Max file size (bytes)</Label><Input type='number' value={String(value('upload', 'max_file_bytes'))} onChange={(e) => update('upload', 'max_file_bytes', Number(e.target.value))} /></div><div className='space-y-2'><Label>Daily upload limit</Label><Input type='number' value={String(value('upload', 'daily_upload_limit'))} onChange={(e) => update('upload', 'daily_upload_limit', Number(e.target.value))} /></div><div className='space-y-2'><Label>Guest daily upload limit</Label><Input type='number' value={String(value('upload', 'guest_daily_upload_limit'))} onChange={(e) => update('upload', 'guest_daily_upload_limit', Number(e.target.value))} /></div><div className='space-y-2'><Label>Guest daily bytes (0 = unlimited)</Label><Input type='number' value={String(value('upload', 'guest_daily_upload_bytes'))} onChange={(e) => update('upload', 'guest_daily_upload_bytes', Number(e.target.value))} /></div><div className='space-y-2'><Label>Guest retention (days, 0 = permanent)</Label><Input type='number' min='0' value={String(value('upload', 'guest_retention_days'))} onChange={(e) => update('upload', 'guest_retention_days', Number(e.target.value))} /></div><div className='space-y-2'><Label>Short code length</Label><Input type='number' min='4' max='32' value={String(value('upload', 'short_code_length'))} onChange={(e) => update('upload', 'short_code_length', Number(e.target.value))} /></div><div className='space-y-2'><Label>File naming</Label><select className='h-9 w-full rounded-md border bg-transparent px-3 text-sm' value={String(value('upload', 'naming_mode'))} onChange={(e) => update('upload', 'naming_mode', e.target.value)}><option value='sha256'>SHA-256</option><option value='md5'>MD5</option><option value='original'>Original name</option><option value='uuid'>UUID-like random</option><option value='random'>Random string</option></select></div><div className='space-y-2'><Label>Directory rule</Label><select className='h-9 w-full rounded-md border bg-transparent px-3 text-sm' value={String(value('upload', 'directory_rule'))} onChange={(e) => update('upload', 'directory_rule', e.target.value)}><option value='hash2'>Hash prefix</option><option value='none'>No directory</option><option value='year'>Year</option><option value='ym'>Year / month</option><option value='ymd'>Year / month / day</option></select></div><div className='space-y-2'><Label>Random name length</Label><Input type='number' min='4' max='128' value={String(value('upload', 'random_length'))} onChange={(e) => update('upload', 'random_length', Number(e.target.value))} /></div><div className='space-y-2 sm:col-span-2'><Label>Path template (optional)</Label><Input placeholder='{year}/{month}/{hash}.{ext}' value={String(value('upload', 'path_template'))} onChange={(e) => update('upload', 'path_template', e.target.value)} /><p className='text-xs text-muted-foreground'>Variables: {'{year}'} {'{month}'} {'{day}'} {'{user_id}'} {'{hash}'} {'{md5}'} {'{uuid}'} {'{random}'} {'{filename}'} {'{ext}'}</p></div></div>
              <div className='grid gap-3 sm:grid-cols-2'><Toggle label='Allow SVG' description='Sanitize active content before serving.' checked={Boolean(value('upload', 'allow_svg'))} onChange={(v) => update('upload', 'allow_svg', v)} /><Toggle label='Allow video' description='Store video and extract metadata asynchronously.' checked={Boolean(value('upload', 'allow_video'))} onChange={(v) => update('upload', 'allow_video', v)} /><Toggle label='Allow remote URL import' description='Block private networks and enforce the remote size limit.' checked={Boolean(value('upload', 'allow_remote_url'))} onChange={(v) => update('upload', 'allow_remote_url', v)} /><Toggle label='Allow anonymous uploads' description='Use a separate IP rate limit and quota.' checked={Boolean(value('upload', 'anonymous_enabled'))} onChange={(v) => update('upload', 'anonymous_enabled', v)} /><Toggle label='Enable short links' description='Return compact /s/{code} URLs for uploaded files.' checked={Boolean(value('upload', 'short_links_enabled'))} onChange={(v) => update('upload', 'short_links_enabled', v)} /></div>
              <Button onClick={() => save('upload')} disabled={saving === 'upload'}>{saving === 'upload' ? 'Saving…' : 'Save upload policy'}</Button>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Email (SMTP)</CardTitle><CardDescription>Secrets are masked when read back. Leave masked fields unchanged.</CardDescription></CardHeader>
            <CardContent className='space-y-4'>
              <div className='grid gap-4 sm:grid-cols-2'><div className='space-y-2'><Label>SMTP host</Label><Input value={String(value('email', 'smtp_host'))} onChange={(e) => update('email', 'smtp_host', e.target.value)} /></div><div className='space-y-2'><Label>SMTP port</Label><Input type='number' value={String(value('email', 'smtp_port'))} onChange={(e) => update('email', 'smtp_port', Number(e.target.value))} /></div><div className='space-y-2'><Label>SMTP username</Label><Input value={String(value('email', 'smtp_username'))} onChange={(e) => update('email', 'smtp_username', e.target.value)} /></div><div className='space-y-2'><Label>SMTP password</Label><Input type='password' placeholder='********' onChange={(e) => update('email', 'smtp_password', e.target.value)} /></div><div className='space-y-2'><Label>From name</Label><Input value={String(value('email', 'from_name'))} onChange={(e) => update('email', 'from_name', e.target.value)} /></div><div className='space-y-2'><Label>From address</Label><Input type='email' value={String(value('email', 'from_address'))} onChange={(e) => update('email', 'from_address', e.target.value)} /></div></div>
              <div className='flex flex-wrap gap-2'><Button onClick={() => save('email')} disabled={saving === 'email'}>{saving === 'email' ? 'Saving…' : 'Save email settings'}</Button><Input className='max-w-xs' type='email' placeholder='Test recipient email' value={testRecipient} onChange={(e) => setTestRecipient(e.target.value)} /><Button variant='outline' disabled={!testRecipient} onClick={() => void sendTestEmail(testRecipient).then((result) => toast.success(result.sent ? 'Test email sent' : 'SMTP is not configured')).catch((error) => toast.error(error instanceof Error ? error.message : 'Test email failed'))}>Send test email</Button></div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><CardTitle>Storage & CDN</CardTitle><CardDescription>Choose local, Telegram or S3-compatible storage and configure the public resource base URL.</CardDescription></CardHeader>
            <CardContent className='space-y-4'>
              <div className='space-y-2'><Label>Storage backend</Label><select className='h-9 w-full rounded-md border bg-transparent px-3 text-sm' value={String(value('storage', 'backend'))} onChange={(e) => update('storage', 'backend', e.target.value)}><option value='local'>Local volume</option><option value='telegram'>Telegram Bot API</option><option value='s3'>S3 / MinIO / R2</option></select></div>
              <div className='space-y-2'><Label>Telegram chat/channel ID</Label><Input value={String(value('storage', 'telegram_chat_id'))} onChange={(e) => update('storage', 'telegram_chat_id', e.target.value)} /><p className='text-xs text-muted-foreground'>The bot token is read from TELEGRAM_BOT_TOKEN; this ID can be saved here.</p></div>
              {String(value('storage', 'backend')) === 's3' && <div className='grid gap-4 rounded-lg border p-4 sm:grid-cols-2'><div className='space-y-2 sm:col-span-2'><Label>S3 endpoint (optional for AWS)</Label><Input placeholder='https://s3.example.com' value={String(value('storage', 's3_endpoint'))} onChange={(e) => update('storage', 's3_endpoint', e.target.value)} /></div><div className='space-y-2'><Label>Region</Label><Input value={String(value('storage', 's3_region'))} onChange={(e) => update('storage', 's3_region', e.target.value)} /></div><div className='space-y-2'><Label>Bucket</Label><Input value={String(value('storage', 's3_bucket'))} onChange={(e) => update('storage', 's3_bucket', e.target.value)} /></div><div className='space-y-2'><Label>Access key</Label><Input value={String(value('storage', 's3_access_key'))} onChange={(e) => update('storage', 's3_access_key', e.target.value)} /></div><div className='space-y-2'><Label>Secret key</Label><Input type='password' placeholder='********' value={String(value('storage', 's3_secret_key'))} onChange={(e) => update('storage', 's3_secret_key', e.target.value)} /></div><div className='space-y-2'><Label>Object prefix</Label><Input placeholder='imagehub' value={String(value('storage', 's3_prefix'))} onChange={(e) => update('storage', 's3_prefix', e.target.value)} /></div><Toggle label='Path-style requests' description='Enable this for MinIO, some R2 gateways and custom S3 endpoints.' checked={Boolean(value('storage', 's3_path_style'))} onChange={(v) => update('storage', 's3_path_style', v)} /></div>}
              <div className='space-y-2'><Label>CDN base URL</Label><Input placeholder='https://cdn.example.com' value={String(value('storage', 'cdn_base_url'))} onChange={(e) => update('storage', 'cdn_base_url', e.target.value)} /><p className='text-xs text-muted-foreground'>Optional public URL prefix used for media links.</p></div><Button onClick={() => save('storage')} disabled={saving === 'storage'}>{saving === 'storage' ? 'Saving…' : 'Save storage settings'}</Button>
            </CardContent>
          </Card>

          <Card className='xl:col-span-2'>
            <CardHeader><CardTitle>OIDC login</CardTitle><CardDescription>External identity is opt-in; local admin login remains available as a recovery path.</CardDescription></CardHeader>
            <CardContent className='space-y-4'><Toggle label='Enable OIDC' description='Show the external login option after discovery settings are complete.' checked={Boolean(value('oidc', 'enabled'))} onChange={(v) => update('oidc', 'enabled', v)} /><Separator /><div className='grid gap-4 sm:grid-cols-2'><div className='space-y-2'><Label>Issuer URL</Label><Input placeholder='https://issuer.example.com' value={String(value('oidc', 'issuer_url'))} onChange={(e) => update('oidc', 'issuer_url', e.target.value)} /></div><div className='space-y-2'><Label>Client ID</Label><Input value={String(value('oidc', 'client_id'))} onChange={(e) => update('oidc', 'client_id', e.target.value)} /></div><div className='space-y-2'><Label>Client secret</Label><Input type='password' placeholder='********' onChange={(e) => update('oidc', 'client_secret', e.target.value)} /></div><div className='space-y-2'><Label>Scopes</Label><Input value={String(value('oidc', 'scopes'))} onChange={(e) => update('oidc', 'scopes', e.target.value)} /></div></div><Toggle label='Auto-create users' description='Create a local profile after a successful OIDC login.' checked={Boolean(value('oidc', 'auto_create_users'))} onChange={(v) => update('oidc', 'auto_create_users', v)} /><Button onClick={() => save('oidc')} disabled={saving === 'oidc'}>{saving === 'oidc' ? 'Saving…' : 'Save OIDC settings'}</Button></CardContent>
          </Card>
          <Card className='xl:col-span-2'>
            <CardHeader><CardTitle>OIDC providers</CardTitle><CardDescription>Each provider uses discovery, PKCE, nonce and signed ID-token validation.</CardDescription></CardHeader>
            <CardContent className='space-y-4'>{providers.map((item) => <div key={item.id} className='flex items-center justify-between rounded-lg border p-3 text-sm'><div><p className='font-medium'>{item.name}</p><p className='text-muted-foreground'>{item.issuer_url} · {item.enabled ? 'Enabled' : 'Disabled'}</p></div><Button variant='ghost' size='icon' onClick={() => void deleteOIDCProvider(item.id).then(() => setProviders((current) => current.filter((providerItem) => providerItem.id !== item.id))).catch((error) => toast.error(error instanceof Error ? error.message : 'Could not remove provider'))}><Trash2 className='h-4 w-4' /></Button></div>)}<div className='grid gap-3 sm:grid-cols-2'><Input placeholder='Provider name' value={provider.name} onChange={(event) => setProvider({ ...provider, name: event.target.value })} /><Input placeholder='Issuer URL' value={provider.issuer_url} onChange={(event) => setProvider({ ...provider, issuer_url: event.target.value })} /><Input placeholder='Client ID' value={provider.client_id} onChange={(event) => setProvider({ ...provider, client_id: event.target.value })} /><Input type='password' placeholder='Client secret' value={provider.client_secret} onChange={(event) => setProvider({ ...provider, client_secret: event.target.value })} /></div><Button onClick={() => void addProvider()} disabled={!provider.name || !provider.issuer_url || !provider.client_id || !provider.client_secret}><Plus className='me-2 h-4 w-4' />Add provider</Button></CardContent>
          </Card>
        </div>
      </Main>
    </>
  )
}
