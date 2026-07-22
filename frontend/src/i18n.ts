import { computed, ref } from 'vue'

export type Locale = 'en' | 'zh'

const storageKey = 'gemcp.locale'

function detectedLocale(): Locale {
  if (typeof window === 'undefined') return 'en'
  try {
    const stored = window.localStorage.getItem(storageKey)
    if (stored === 'en' || stored === 'zh') return stored
  } catch {
    // Browser privacy settings can make localStorage unavailable.
  }
  return window.navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

const locale = ref<Locale>(detectedLocale())
const languageTag = computed(() => locale.value === 'zh' ? 'zh-CN' : 'en')

function applyLocale(value: Locale) {
  locale.value = value
  if (typeof document !== 'undefined') document.documentElement.lang = value === 'zh' ? 'zh-CN' : 'en'
  if (typeof window !== 'undefined') {
    try {
      window.localStorage.setItem(storageKey, value)
    } catch {
      // Keep the in-memory preference when persistence is unavailable.
    }
  }
}

applyLocale(locale.value)

export function useI18n() {
  function t(english: string, chinese: string) {
    return locale.value === 'zh' ? chinese : english
  }

  function setLocale(value: Locale) {
    applyLocale(value)
  }

  function toggleLocale() {
    applyLocale(locale.value === 'en' ? 'zh' : 'en')
  }

  return { locale, languageTag, t, setLocale, toggleLocale }
}

const stateTranslations: Record<string, string> = {
  active: '活跃',
  approved: '已批准',
  available: '可用',
  cancelled: '已取消',
  cancelling: '正在取消',
  claimed: '待批准',
  collecting: '正在收集',
  completed: '已完成',
  configured: '已配置',
  deleted: '已删除',
  disabled: '已禁用',
  error: '错误',
  expired: '已过期',
  failed: '失败',
  finished: '已完成',
  in_cache: '缓存中',
  lost: '已失联',
  managed: '受管',
  not_connected: '未连接',
  offline: '离线',
  online: '在线',
  pending: '等待中',
  pending_validation: '等待验证',
  provisioning: '正在配置',
  quarantined: '已隔离',
  queued: '排队中',
  ready: '就绪',
  revoked: '已撤销',
  running: '运行中',
  sending: '发送中',
  sent: '已发送',
  shutdown: '已关闭',
  starting: '正在启动',
  stopped: '已停止',
  stopping: '正在停止',
  succeeded: '成功',
  timed_out: '已超时',
  unavailable: '不可用',
}

export function localizedState(value: string, currentLocale = locale.value) {
  const normalized = value.toLowerCase().replaceAll(' ', '_')
  if (currentLocale === 'zh') return stateTranslations[normalized] ?? value.replaceAll('_', ' ')
  return value.replaceAll('_', ' ')
}
