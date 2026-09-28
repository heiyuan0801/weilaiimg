import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type Locale = 'zh-CN' | 'en-US'
type Messages = Record<string, string>

const messages: Record<Locale, Messages> = {
  'en-US': {
    'brand.name': 'ImageHub', 'nav.signIn': 'Sign in', 'nav.register': 'Register', 'nav.signOut': 'Sign out', 'nav.language': '中文',
    'nav.library': 'Library', 'nav.allMedia': 'All media', 'nav.administration': 'Administration', 'nav.personal': 'Personal', 'nav.dashboard': 'Dashboard', 'nav.images': 'Image library', 'nav.teams': 'Teams', 'nav.billing': 'Billing', 'nav.domains': 'Custom domains', 'nav.users': 'Users', 'nav.settings': 'Settings', 'nav.profile': 'Profile', 'nav.account': 'Account', 'nav.appearance': 'Appearance', 'nav.notifications': 'Notifications', 'nav.display': 'Display', 'nav.system': 'System settings',
    'home.badge': 'Image hosting', 'home.title': 'Upload and share in seconds', 'home.subtitle': 'Drag an image here, paste from your clipboard, or choose multiple files. Sign in later to manage your library.', 'home.loginAction': 'Sign in to manage', 'home.registerAction': 'Create a free account', 'home.featureMediaTitle': 'Images and video', 'home.featureMediaDescription': 'Support images, SVG, GIF, video and remote URL imports.', 'home.featureStorageTitle': 'Flexible storage', 'home.featureStorageDescription': 'Use local storage or Telegram storage with CDN and custom domains.', 'home.featureTeamTitle': 'Teams and quotas', 'home.featureTeamDescription': 'Collaborate with teams, usage quotas and billing controls.',
    'upload.title': 'Upload images', 'upload.disabled': 'Guest uploads are currently disabled; sign in to upload.', 'upload.drop': 'Drop files here or click to browse', 'upload.paste': 'You can also press Ctrl/⌘ + V to paste an image', 'upload.results': 'Upload results', 'upload.copyAll': 'Copy all URLs', 'upload.copied': 'Copied', 'upload.ready': 'Ready to share', 'upload.guestEnabled': 'Guest uploads are enabled', 'upload.expiresAfter': 'links expire after', 'upload.video': 'Video', 'upload.openFile': 'Open file', 'upload.copy': 'Copy', 'upload.clipboardUnavailable': 'Clipboard access is unavailable',
    'auth.email': 'Email', 'auth.emailOrUsername': 'Email or username', 'auth.password': 'Password', 'auth.forgot': 'Forgot password?', 'auth.remember': 'Remember me', 'auth.signIn': 'Sign in', 'auth.continue': 'Or continue with', 'auth.username': 'Username', 'auth.confirmPassword': 'Confirm password', 'auth.createAccount': 'Create account',
    'dashboard.title': 'ImageHub overview', 'dashboard.description': 'Your current library, storage and collaboration usage.', 'dashboard.media': 'Media files', 'dashboard.storage': 'Storage used', 'dashboard.teams': 'Teams', 'dashboard.quickUpload': 'Quick upload', 'dashboard.openLibrary': 'Open library', 'dashboard.manageTeams': 'Manage teams', 'dashboard.uploadMedia': 'Upload media', 'dashboard.storageCapacity': 'Storage capacity', 'dashboard.used': 'used', 'dashboard.quota': 'quota', 'dashboard.adminMetrics': 'Administrator metrics', 'dashboard.users': 'Registered users', 'dashboard.newUsersToday': 'New today', 'dashboard.activeUsers': 'Active users', 'dashboard.pendingUsers': 'Pending', 'dashboard.disabledUsers': 'Disabled', 'dashboard.files': 'Media files', 'dashboard.physicalFiles': 'Physical objects', 'dashboard.mediaBreakdown': 'Images / videos', 'dashboard.imagesVideos': 'image count / video count', 'dashboard.todayUploads': 'Today uploads', 'dashboard.registeredUploadShare': 'Registered upload share', 'dashboard.guestUploads': 'Guest uploads',
    'adminMedia.title': 'All media', 'adminMedia.description': 'Search by filename, SHA-256 hash, MIME type or owner.', 'adminMedia.refresh': 'Refresh', 'adminMedia.inventory': 'Media inventory', 'adminMedia.inventoryDescription': 'Force delete keeps shared objects until its last reference is removed.', 'adminMedia.search': 'Search', 'adminMedia.preview': 'Preview', 'adminMedia.file': 'File', 'adminMedia.owner': 'Owner', 'adminMedia.typeSize': 'Type / size', 'adminMedia.storageRefs': 'Storage / refs', 'adminMedia.created': 'Created', 'adminMedia.action': 'Action', 'adminMedia.noResults': 'No media found.', 'adminMedia.guest': 'Guest', 'adminMedia.open': 'Open', 'adminMedia.searchPlaceholder': 'Filename, hash or owner email', 'adminMedia.mimePlaceholder': 'MIME type, e.g. image/png', 'adminMedia.deleteConfirm': 'Force delete this media? Shared objects are removed only after their last reference is gone.', 'adminMedia.deleted': 'Media deleted', 'adminMedia.viewMode': 'View mode', 'adminMedia.listView': 'List', 'adminMedia.gridView': 'Images',
    'images.title': 'Image library', 'images.description': 'Upload images, SVG, GIF and video, or import a remote URL.', 'images.upload': 'Upload files', 'images.uploadDescription': 'Upload multiple files at once. Files are validated and stored by the server.', 'images.remote': 'Import remote URL', 'images.remoteDescription': 'Private networks and oversized responses are blocked by the API.', 'images.choose': 'Choose files or drag them here', 'images.import': 'Import', 'images.noMedia': 'No media yet. Upload your first file to get started.', 'images.search': 'Search filename or MIME type', 'images.filter': 'Filter type', 'images.allTypes': 'All types', 'images.images': 'Images', 'images.videos': 'Videos', 'images.clearFilters': 'Clear filters', 'images.previous': 'Previous', 'images.next': 'Next',
    'users.title': 'Users', 'users.description': 'Set quota, per-file limit, daily limits and per-user naming strategy.', 'users.refresh': 'Refresh', 'users.accounts': 'accounts', 'users.policySaved': 'User policy saved', 'users.noAccounts': 'No accounts found or administrator access is required.', 'users.save': 'Save',
  },
  'zh-CN': {
    'brand.name': '图床', 'nav.signIn': '登录', 'nav.register': '注册', 'nav.signOut': '退出登录', 'nav.language': 'English',
    'nav.library': '内容库', 'nav.allMedia': '全部媒体', 'nav.administration': '管理', 'nav.personal': '个人中心', 'nav.dashboard': '仪表盘', 'nav.images': '图片库', 'nav.teams': '团队', 'nav.billing': '计费', 'nav.domains': '自定义域名', 'nav.users': '用户管理', 'nav.settings': '系统设置', 'nav.profile': '个人资料', 'nav.account': '账号', 'nav.appearance': '外观', 'nav.notifications': '通知', 'nav.display': '显示', 'nav.system': '系统设置',
    'home.badge': '图床服务', 'home.title': '几秒钟上传并分享', 'home.subtitle': '拖拽图片、从剪贴板粘贴，或一次选择多个文件。登录后可以管理自己的图片库。', 'home.loginAction': '登录后管理', 'home.registerAction': '免费注册', 'home.featureMediaTitle': '图片和视频', 'home.featureMediaDescription': '支持图片、SVG、GIF、视频和远程 URL 导入。', 'home.featureStorageTitle': '灵活存储', 'home.featureStorageDescription': '支持本地存储或 Telegram 存储，并可配置 CDN 和自定义域名。', 'home.featureTeamTitle': '团队和配额', 'home.featureTeamDescription': '支持团队协作、用量配额和计费管理。',
    'upload.title': '上传图片', 'upload.disabled': '游客上传已关闭，请先登录。', 'upload.drop': '拖拽文件到这里，或点击选择', 'upload.paste': '也可以按 Ctrl/⌘ + V 粘贴图片', 'upload.results': '上传结果', 'upload.copyAll': '复制全部 URL', 'upload.copied': '已复制', 'upload.ready': '可以分享', 'upload.guestEnabled': '游客上传已开启', 'upload.expiresAfter': '链接将在以下时间后过期', 'upload.video': '视频', 'upload.openFile': '打开文件', 'upload.copy': '复制', 'upload.clipboardUnavailable': '无法访问剪贴板',
    'auth.email': '邮箱', 'auth.emailOrUsername': '邮箱或用户名', 'auth.password': '密码', 'auth.forgot': '忘记密码？', 'auth.remember': '记住登录状态', 'auth.signIn': '登录', 'auth.continue': '或使用以下方式继续', 'auth.username': '用户名', 'auth.confirmPassword': '确认密码', 'auth.createAccount': '创建账号',
    'dashboard.title': '图床概览', 'dashboard.description': '查看当前图片库、存储和协作使用情况。', 'dashboard.media': '媒体文件', 'dashboard.storage': '已使用空间', 'dashboard.teams': '团队', 'dashboard.quickUpload': '快速上传', 'dashboard.openLibrary': '打开图片库', 'dashboard.manageTeams': '管理团队', 'dashboard.uploadMedia': '上传媒体', 'dashboard.storageCapacity': '存储容量', 'dashboard.used': '已使用', 'dashboard.quota': '配额', 'dashboard.adminMetrics': '管理员指标', 'dashboard.users': '注册用户', 'dashboard.newUsersToday': '今日新增', 'dashboard.activeUsers': '活跃用户', 'dashboard.pendingUsers': '待验证', 'dashboard.disabledUsers': '已停用', 'dashboard.files': '媒体文件', 'dashboard.physicalFiles': '实际对象', 'dashboard.mediaBreakdown': '图片 / 视频', 'dashboard.imagesVideos': '图片数量 / 视频数量', 'dashboard.todayUploads': '今日上传', 'dashboard.registeredUploadShare': '注册用户上传占比', 'dashboard.guestUploads': '游客上传',
    'adminMedia.title': '全部媒体', 'adminMedia.description': '按文件名、SHA-256、MIME 类型或所有者搜索。', 'adminMedia.refresh': '刷新', 'adminMedia.inventory': '媒体清单', 'adminMedia.inventoryDescription': '强制删除会在最后一个引用移除后再删除共享对象。', 'adminMedia.search': '搜索', 'adminMedia.preview': '预览', 'adminMedia.file': '文件', 'adminMedia.owner': '所有者', 'adminMedia.typeSize': '类型 / 大小', 'adminMedia.storageRefs': '存储 / 引用', 'adminMedia.created': '创建时间', 'adminMedia.action': '操作', 'adminMedia.noResults': '没有找到媒体。', 'adminMedia.guest': '游客', 'adminMedia.open': '打开', 'adminMedia.searchPlaceholder': '文件名、哈希或所有者邮箱', 'adminMedia.mimePlaceholder': 'MIME 类型，例如 image/png', 'adminMedia.deleteConfirm': '确定强制删除此媒体吗？共享对象会在最后一个引用移除后删除。', 'adminMedia.deleted': '媒体已删除', 'adminMedia.viewMode': '查看模式', 'adminMedia.listView': '列表', 'adminMedia.gridView': '图片',
    'images.title': '图片库', 'images.description': '上传图片、SVG、GIF 和视频，或导入远程 URL。', 'images.upload': '上传文件', 'images.uploadDescription': '支持一次上传多个文件，服务端会校验并保存。', 'images.remote': '导入远程 URL', 'images.remoteDescription': 'API 会阻止私有网络地址和超大响应。', 'images.choose': '选择文件或拖拽到这里', 'images.import': '导入', 'images.noMedia': '还没有媒体，上传第一个文件开始使用。', 'images.search': '搜索文件名或 MIME 类型', 'images.filter': '筛选类型', 'images.allTypes': '全部类型', 'images.images': '图片', 'images.videos': '视频', 'images.clearFilters': '清除筛选', 'images.previous': '上一页', 'images.next': '下一页',
    'users.title': '用户管理', 'users.description': '为每个账号设置容量、单文件限制、每日限制和命名策略。', 'users.refresh': '刷新', 'users.accounts': '个账号', 'users.policySaved': '用户策略已保存', 'users.noAccounts': '没有找到用户，或当前账号没有管理员权限。', 'users.save': '保存',
  },
}

const storageKey = 'imagehub-locale'
function browserLocale(): Locale | null {
  if (typeof navigator === 'undefined') return null
  const languages = navigator.languages?.length ? navigator.languages : [navigator.language]
  if (languages.some((value) => value.toLowerCase().startsWith('zh'))) return 'zh-CN'
  if (languages.some((value) => value.toLowerCase().startsWith('en'))) return 'en-US'
  return null
}
function initialLocale(): Locale {
  if (typeof window === 'undefined') return 'en-US'
  const stored = window.localStorage.getItem(storageKey)
  return stored === 'zh-CN' || stored === 'en-US' ? stored : browserLocale() ?? 'en-US'
}

type I18nValue = { locale: Locale; setLocale: (locale: Locale) => void; t: (key: string, fallback?: string) => string }
const I18nContext = createContext<I18nValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale)
  const setLocale = useCallback((next: Locale) => { setLocaleState(next); window.localStorage.setItem(storageKey, next) }, [])
  const value = useMemo<I18nValue>(() => ({ locale, setLocale, t: (key, fallback = key) => messages[locale][key] ?? messages['en-US'][key] ?? fallback }), [locale, setLocale])
  useEffect(() => { document.documentElement.lang = locale }, [locale])
  useEffect(() => {
    const updateFromBrowser = () => { if (!window.localStorage.getItem(storageKey)) { const next = browserLocale(); if (next) setLocaleState(next) } }
    window.addEventListener('languagechange', updateFromBrowser)
    return () => window.removeEventListener('languagechange', updateFromBrowser)
  }, [])
  useEffect(() => {
    if (window.localStorage.getItem(storageKey) || browserLocale() !== null) return
    void fetch('/api/v1/public/config').then((response) => response.ok ? response.json() as Promise<{ default_language?: string }> : null).then((config) => {
      if (config?.default_language === 'zh-CN' || config?.default_language === 'en-US') setLocaleState(config.default_language)
    }).catch(() => undefined)
  }, [])
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n() {
  const value = useContext(I18nContext)
  if (!value) throw new Error('useI18n must be used inside I18nProvider')
  return value
}
