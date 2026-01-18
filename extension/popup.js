const API_BASE = 'http://127.0.0.1:8080/api'

const focusedWindowEl = document.getElementById('focusedWindow')
const allowlistStatusEl = document.getElementById('allowlistStatus')
const browserAllowedEl = document.getElementById('browserAllowed')
const browserContextEl = document.getElementById('browserContext')
const browserUpdatedEl = document.getElementById('browserUpdated')
const browserTTLEl = document.getElementById('browserTTL')
const tabTitleEl = document.getElementById('tabTitle')
const tabUrlEl = document.getElementById('tabUrl')
const ruleHintEl = document.getElementById('ruleHint')
const browserClassEl = document.getElementById('browserClass')
const messageEl = document.getElementById('message')

const allowPageBtn = document.getElementById('allowPage')
const allowDomainBtn = document.getElementById('allowDomain')
const allowSubdomainBtn = document.getElementById('allowSubdomain')
const removeRuleBtn = document.getElementById('removeRule')
const toggleBrowserBtn = document.getElementById('toggleBrowser')

let currentTab = null
let currentWindow = null
let currentConfig = null
let matchingRule = null
let browserWindowClass = null
let browserStatus = null

const logMessage = (text, isError = false) => {
  messageEl.textContent = text
  messageEl.style.color = isError ? '#b91c1c' : '#111827'
}

const fetchJSON = async (url, options) => {
  const response = await fetch(url, options)
  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Request failed: ${response.status}`)
  }
  return response.json()
}

const getActiveTab = async () => {
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true })
  return tabs[0] || null
}

const normalizeURL = (raw) => {
  const url = new URL(raw)
  url.hash = ''
  return url.toString()
}

const getDomainFromHost = (host) => {
  const parts = host.split('.').filter(Boolean)
  if (parts.length <= 2) {
    return host
  }
  return parts.slice(-2).join('.')
}

const ruleMatchesURL = (rule, urlValue) => {
  let parsed
  try {
    parsed = new URL(urlValue)
  } catch (err) {
    return false
  }

  const host = parsed.hostname.toLowerCase()
  const pattern = (rule.pattern || '').trim().toLowerCase()
  if (!pattern || !host) {
    return false
  }

  if (rule.type === 'page') {
    try {
      return normalizeURL(rule.pattern) === normalizeURL(urlValue)
    } catch (err) {
      return false
    }
  }

  if (rule.type === 'domain') {
    const normalizedPattern = pattern.replace(/^\./, '')
    return host === normalizedPattern || host.endsWith(`.${normalizedPattern}`)
  }

  if (rule.type === 'subdomain') {
    const normalizedPattern = pattern.replace(/^\./, '')
    return host === normalizedPattern
  }

  return false
}

const updateUI = () => {
  if (currentWindow) {
    focusedWindowEl.textContent = currentWindow.class || currentWindow.title || 'Unknown'
    browserClassEl.textContent = browserWindowClass || currentWindow.class || 'Unknown'
  } else {
    focusedWindowEl.textContent = 'No focused window'
    browserClassEl.textContent = browserWindowClass || 'Unknown'
  }

  if (currentTab) {
    tabTitleEl.textContent = currentTab.title || 'Untitled'
    tabUrlEl.textContent = currentTab.url || 'Unknown'
  }

  if (currentConfig && browserWindowClass) {
    const blocked = (currentConfig.browser_blocked_classes || []).includes(browserWindowClass)
    browserAllowedEl.textContent = blocked ? 'Blocked' : 'Allowed'
  } else {
    browserAllowedEl.textContent = 'Unknown'
  }

  if (browserStatus) {
    browserContextEl.textContent = browserStatus.fresh ? 'Fresh' : 'Stale'
    const updatedAt = browserStatus.updated_at ? new Date(browserStatus.updated_at) : null
    browserUpdatedEl.textContent = updatedAt ? updatedAt.toLocaleTimeString() : 'Unknown'
    if (typeof browserStatus.ttl_remaining_seconds === 'number') {
      browserTTLEl.textContent = `${browserStatus.ttl_remaining_seconds.toFixed(1)}s`
    } else {
      browserTTLEl.textContent = 'Unknown'
    }
  } else {
    browserContextEl.textContent = 'Unknown'
    browserUpdatedEl.textContent = 'Unknown'
    browserTTLEl.textContent = 'Unknown'
  }

  if (matchingRule) {
    ruleHintEl.textContent = `Matched rule: ${matchingRule.type} (${matchingRule.pattern})`
    removeRuleBtn.disabled = false
  } else {
    ruleHintEl.textContent = 'No matching URL rule.'
    removeRuleBtn.disabled = true
  }
}

const loadData = async () => {
  try {
    currentTab = await getActiveTab()
    currentWindow = await fetchJSON(`${API_BASE}/window/current`).catch(() => null)
    currentConfig = await fetchJSON(`${API_BASE}/config`).catch(() => null)

    const allowlistStatus = await fetchJSON(`${API_BASE}/window/allowlist-status`).catch(() => null)
    if (allowlistStatus) {
      allowlistStatusEl.textContent = allowlistStatus.allowlisted
        ? `Allowed (${allowlistStatus.allowlist_source || 'unknown'})`
        : 'Blocked'
    } else {
      allowlistStatusEl.textContent = 'Unknown'
    }

    if (!browserWindowClass) {
      const stored = await chrome.storage.local.get('browserWindowClass')
      browserWindowClass = stored.browserWindowClass || null
    }

    if (!browserWindowClass && currentWindow && currentWindow.class) {
      browserWindowClass = currentWindow.class
      await chrome.storage.local.set({ browserWindowClass })
    }

    if (browserWindowClass) {
      browserStatus = await fetchJSON(`${API_BASE}/browser/status?window_class=${encodeURIComponent(browserWindowClass)}`).catch(() => null)
    } else {
      browserStatus = null
    }

    if (currentTab && currentConfig) {
      const rules = currentConfig.allowlist_url_rules || []
      matchingRule = rules.find((rule) => ruleMatchesURL(rule, currentTab.url)) || null
    }

    updateUI()
  } catch (err) {
    logMessage(err.message, true)
  }
}

const addRule = async (type) => {
  if (!currentTab || !currentTab.url) {
    logMessage('No active tab URL.', true)
    return
  }

  let pattern = ''
  try {
    const parsed = new URL(currentTab.url)
    if (type === 'page') {
      pattern = normalizeURL(currentTab.url)
    } else if (type === 'domain') {
      pattern = getDomainFromHost(parsed.hostname)
    } else if (type === 'subdomain') {
      pattern = parsed.hostname
    }
  } catch (err) {
    logMessage('Invalid tab URL.', true)
    return
  }

  try {
    await fetchJSON(`${API_BASE}/config/url-rules`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type, pattern })
    })
    logMessage(`Added ${type} rule: ${pattern}`)
    await loadData()
  } catch (err) {
    logMessage(err.message, true)
  }
}

const removeRule = async () => {
  if (!matchingRule) {
    return
  }

  try {
    await fetchJSON(`${API_BASE}/config/url-rules/${matchingRule.id}`, {
      method: 'DELETE'
    })
    logMessage('Removed matching rule')
    await loadData()
  } catch (err) {
    logMessage(err.message, true)
  }
}

const toggleBrowserAllow = async () => {
  if (!browserWindowClass) {
    logMessage('No browser class detected yet.', true)
    return
  }

  const blocked = (currentConfig?.browser_blocked_classes || []).includes(browserWindowClass)
  const allowed = blocked

  try {
    await fetchJSON(`${API_BASE}/browser/allowlist`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ window_class: browserWindowClass, allowed })
    })
    logMessage(allowed ? 'Browser allowed.' : 'Browser blocked.')
    await loadData()
  } catch (err) {
    logMessage(err.message, true)
  }
}

allowPageBtn.addEventListener('click', () => addRule('page'))
allowDomainBtn.addEventListener('click', () => addRule('domain'))
allowSubdomainBtn.addEventListener('click', () => addRule('subdomain'))
removeRuleBtn.addEventListener('click', removeRule)
toggleBrowserBtn.addEventListener('click', toggleBrowserAllow)

loadData()
