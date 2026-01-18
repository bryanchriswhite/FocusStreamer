const API_BASE = 'http://127.0.0.1:8080/api'
const CONTEXT_ENDPOINT = `${API_BASE}/browser/active`
const WINDOW_ENDPOINT = `${API_BASE}/window/current`

const storageKeys = {
  browserWindowClass: 'browserWindowClass'
}

const getStoredBrowserClass = async () => {
  const stored = await chrome.storage.local.get(storageKeys.browserWindowClass)
  return stored[storageKeys.browserWindowClass] || null
}

const setStoredBrowserClass = async (value) => {
  await chrome.storage.local.set({ [storageKeys.browserWindowClass]: value })
}

const resolveBrowserClass = async () => {
  const storedClass = await getStoredBrowserClass()
  if (storedClass) {
    return storedClass
  }

  const fetchedClass = await fetchWindowClass()
  if (fetchedClass) {
    await setStoredBrowserClass(fetchedClass)
    return fetchedClass
  }

  return null
}

const getActiveTab = async () => {
  const tabs = await chrome.tabs.query({ active: true, lastFocusedWindow: true })
  return tabs[0] || null
}

const fetchWindowClass = async () => {
  try {
    const response = await fetch(WINDOW_ENDPOINT)
    if (!response.ok) {
      return null
    }
    const data = await response.json()
    if (data?.class) {
      return data.class
    }
  } catch (err) {
    return null
  }
  return null
}

const postBrowserContext = async (tab) => {
  if (!tab || !tab.url) {
    return
  }

  const windowClass = await resolveBrowserClass()

  if (!windowClass) {
    return
  }

  try {
    await fetch(CONTEXT_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        window_class: windowClass,
        url: tab.url,
        title: tab.title || ''
      })
    })
  } catch (err) {
    // Ignore network errors; popup will show status.
  }
}

const updateContext = async () => {
  const tab = await getActiveTab()
  await postBrowserContext(tab)
}

const ensureOffscreen = async () => {
  if (!chrome.offscreen) {
    return
  }

  const exists = await chrome.offscreen.hasDocument()
  if (exists) {
    return
  }

  await chrome.offscreen.createDocument({
    url: 'offscreen.html',
    reasons: ['DOM_PARSER'],
    justification: 'Keep browser URL context fresh for FocusStreamer.'
  })
}

chrome.runtime.onInstalled.addListener(() => {
  ensureOffscreen()
})

chrome.runtime.onStartup.addListener(() => {
  ensureOffscreen()
})

chrome.runtime.onMessage.addListener((message) => {
  if (message?.type === 'heartbeat') {
    updateContext()
  }
  if (message?.type === 'refresh') {
    updateContext()
  }
})

chrome.tabs.onActivated.addListener(() => {
  updateContext()
})

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.url || changeInfo.title) {
    postBrowserContext(tab)
  }
})

chrome.windows.onFocusChanged.addListener(() => {
  updateContext()
})
