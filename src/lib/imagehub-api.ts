const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

export class ImageHubApiError extends Error {
  readonly status: number
  readonly code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ImageHubApiError'
    this.status = status
    this.code = code
  }
}

export async function imageHubFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
    ...init,
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({} as { message?: string; code?: string }))
    throw new ImageHubApiError(body.message ?? `Request failed (${response.status})`, response.status, body.code)
  }
  return response.json() as Promise<T>
}

export type SystemSettings = Record<string, Record<string, unknown>>

export type PublicSiteConfig = {
  site_name: string
  logo_url: string
  favicon_url: string
  default_language: string
}

export function getPublicSiteConfig() {
  return imageHubFetch<PublicSiteConfig>('/api/v1/site/config')
}

export function login(input: { email: string; password: string; remember?: boolean }) {
  return imageHubFetch<{ user_id: string; role: string }>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
export type PublicOIDCProvider = { id: string; name: string }
export function listPublicOIDCProviders() { return imageHubFetch<PublicOIDCProvider[]>('/api/v1/auth/oidc/providers') }

export type CurrentUser = { id: string; email: string; username?: string; display_name?: string; role: string; status: string }
export function getCurrentUser() { return imageHubFetch<CurrentUser>('/api/v1/auth/me') }

export function logout() {
  return imageHubFetch<{ ok: boolean }>('/api/v1/auth/logout', { method: 'POST' })
}

export function register(input: { email: string; username?: string; password: string; display_name?: string }) {
  return imageHubFetch<{ id: string; status: string; email_verification_required: boolean }>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function forgotPassword(email: string) {
  return imageHubFetch<{ message: string; email_delivery?: string }>('/api/v1/auth/forgot-password', {
    method: 'POST',
    body: JSON.stringify({ email }),
  })
}

export function resetPassword(input: { token: string; password: string }) {
  return imageHubFetch<{ reset: boolean }>('/api/v1/auth/reset-password', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function verifyEmail(token: string) {
  return imageHubFetch<{ verified: boolean }>(`/api/v1/auth/verify-email?token=${encodeURIComponent(token)}`)
}

export type Team = {
  id: string
  name: string
  slug: string
  role: string
  quota_bytes: number
  used_bytes: number
  plan_code: string
}

export type TeamMember = {
  user_id: string
  email: string
  display_name?: string | null
  role: string
  created_at?: string
}

export type TeamInvitation = {
  id: string
  email: string
  role: string
  status?: string
  expires_at: string
  accepted_at?: string | null
  created_at?: string
  invite_url?: string
}

export type Plan = {
  code: string
  name: string
  price_cents: number
  quota_bytes: number
  bandwidth_bytes: number
  member_limit: number
  features: string[] | Record<string, unknown> | null
}

export type Subscription = {
  id?: string
  team_id: string
  plan_code: string
  status: string
  provider?: string | null
  provider_subscription_id?: string | null
  current_period_end?: string | null
}

export type CustomDomain = {
  id: string
  domain: string
  verified_at: string | null
  tls_status: string
  is_default: boolean
  verification?: { type: string; name: string; value: string }
}

export function getSystemSettings() {
  return imageHubFetch<SystemSettings>('/api/v1/admin/settings')
}

export function saveSystemSetting(key: string, value: Record<string, unknown>) {
  return imageHubFetch<{ key: string; value: Record<string, unknown> }>(
    `/api/v1/admin/settings/${key}`,
    { method: 'PATCH', body: JSON.stringify(value) }
  )
}

export function sendTestEmail(recipient: string) {
  return imageHubFetch<{ sent: boolean; delivery: string }>('/api/v1/admin/email/test', {
    method: 'POST',
    body: JSON.stringify({ recipient }),
  })
}

export type StorageChannel = { id: string; name: string; backend: string; is_default: boolean }
export function listStorageChannels() {
  return imageHubFetch<StorageChannel[]>('/api/v1/storage/channels')
}

export function testStorage(channelId?: string) {
  return imageHubFetch<{ ok: boolean; backend: string; channel_id: string }>('/api/v1/admin/storage/test', { method: 'POST', body: JSON.stringify(channelId ? { channel_id: channelId } : {}) })
}

export function listTeams() {
  return imageHubFetch<Team[]>('/api/v1/teams')
}

export function createTeam(input: { name: string; slug?: string }) {
  return imageHubFetch<Team>('/api/v1/teams', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function listTeamMembers(teamId: string) {
  return imageHubFetch<TeamMember[]>(`/api/v1/teams/${teamId}/members`)
}

export function listTeamInvitations(teamId: string) {
  return imageHubFetch<TeamInvitation[]>(`/api/v1/teams/${teamId}/invitations`)
}

export function createTeamInvitation(teamId: string, input: { email: string; role?: string }) {
  return imageHubFetch<TeamInvitation>(`/api/v1/teams/${teamId}/invitations`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function listPlans() {
  return imageHubFetch<Plan[]>('/api/v1/billing/plans')
}

export function upsertSubscription(teamId: string, planCode: string) {
  return imageHubFetch<Subscription>(`/api/v1/teams/${teamId}/billing/subscription`, {
    method: 'PUT',
    body: JSON.stringify({ plan_code: planCode, status: 'active' }),
  })
}

export function listDomains() {
  return imageHubFetch<CustomDomain[]>('/api/v1/domains')
}

export function createDomain(domain: string) {
  return imageHubFetch<CustomDomain>('/api/v1/domains', {
    method: 'POST',
    body: JSON.stringify({ domain }),
  })
}

export function verifyDomain(domainId: string) {
  return imageHubFetch<{ id: string; domain: string; verified: boolean; tls_status: string }>(`/api/v1/domains/${domainId}/verify`, {
    method: 'POST',
  })
}

export function deleteDomain(domainId: string) {
  return imageHubFetch<{ deleted: boolean }>(`/api/v1/domains/${domainId}`, { method: 'DELETE' })
}

export function createCheckoutSession(teamId: string, planCode: string) {
  return imageHubFetch<{ id: string; url: string }>(`/api/v1/teams/${teamId}/billing/checkout`, {
    method: 'POST', body: JSON.stringify({ plan_code: planCode }),
  })
}

export function acceptTeamInvitation(token: string) {
  return imageHubFetch<{ accepted: boolean; team_id: string }>(`/api/v1/team-invitations/${encodeURIComponent(token)}/accept`, { method: 'POST' })
}

export function uploadImage(file: File, options: { onProgress?: (progress: number) => void; teamId?: string; storageChannel?: string } = {}) {
  const body = new FormData(); body.append('file', file); if (options.teamId) body.append('team_id', options.teamId); if (options.storageChannel) body.append('storage_channel', options.storageChannel)
  return new Promise<Record<string, unknown>>((resolve, reject) => {
    const request = new XMLHttpRequest(); request.open('POST', `${API_BASE}/api/v1/images/upload`); request.withCredentials = true
    request.upload.onprogress = (event) => { if (event.lengthComputable) options.onProgress?.(Math.round(event.loaded / event.total * 100)) }
    request.onerror = () => reject(new Error('Upload failed'))
    request.onload = () => { let value: Record<string, unknown> = {}; try { value = JSON.parse(request.responseText) } catch { /* handled below */ }; if (request.status >= 200 && request.status < 300) resolve(value); else reject(new Error(String(value.message ?? `Upload failed (${request.status})`))) }
    request.send(body)
  })
}

export type GuestUploadResult = {
  id: string
  url: string
  short_url?: string
  name: string
  mime_type: string
  size_bytes: number
  metadata?: { width?: number; height?: number; [key: string]: unknown }
  expires_at?: string
  thumbnail_url?: string
}

export function uploadGuestImage(file: File, onProgress?: (progress: number) => void) {
  const body = new FormData()
  body.append('file', file)
  return new Promise<GuestUploadResult>((resolve, reject) => {
    const request = new XMLHttpRequest()
    request.open('POST', `${API_BASE}/api/v1/public/upload`)
    request.withCredentials = true
    request.upload.onprogress = (event) => { if (event.lengthComputable) onProgress?.(Math.round(event.loaded / event.total * 100)) }
    request.onerror = () => reject(new Error('Upload failed'))
    request.onload = () => {
      let value: Record<string, unknown> = {}
      try { value = JSON.parse(request.responseText) } catch { /* handled below */ }
      if (request.status >= 200 && request.status < 300) resolve(value as unknown as GuestUploadResult)
      else reject(new Error(String(value.message ?? `Upload failed (${request.status})`)))
    }
    request.send(body)
  })
}

export function importImageURL(url: string, teamId?: string, storageChannel?: string) {
  return imageHubFetch<Record<string, unknown>>('/api/v1/images/import-url', { method: 'POST', body: JSON.stringify({ url, ...(teamId ? { team_id: teamId } : {}), ...(storageChannel ? { storage_channel: storageChannel } : {}) }) })
}

export type OIDCProvider = { id: string; name: string; issuer_url: string; client_id: string; scopes: string[]; enabled: boolean; auto_create_users: boolean }
export function listOIDCProviders() { return imageHubFetch<OIDCProvider[]>('/api/v1/oidc/providers') }
export function createOIDCProvider(input: { name: string; issuer_url: string; client_id: string; client_secret: string; scopes?: string[]; enabled?: boolean; auto_create_users?: boolean }) { return imageHubFetch<OIDCProvider>('/api/v1/oidc/providers', { method: 'POST', body: JSON.stringify(input) }) }
export function deleteOIDCProvider(id: string) { return imageHubFetch<{ deleted: boolean }>(`/api/v1/oidc/providers/${id}`, { method: 'DELETE' }) }

export function updateImageVisibility(imageId: string, visibility: 'private' | 'public' | 'link') {
  return imageHubFetch<{ id: string; visibility: string; url: string }>(`/api/v1/images/${imageId}`, {
    method: 'PATCH',
    body: JSON.stringify({ visibility }),
  })
}
