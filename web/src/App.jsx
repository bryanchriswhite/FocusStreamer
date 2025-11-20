import { useState, useEffect } from 'react'
import './App.css'
import ApplicationList from './components/ApplicationList'
import CurrentWindow from './components/CurrentWindow'
import PatternManager from './components/PatternManager'

function App() {
  const [applications, setApplications] = useState([])
  const [currentWindow, setCurrentWindow] = useState(null)
  const [config, setConfig] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  // Fetch applications
  const fetchApplications = async () => {
    try {
      const response = await fetch('/api/applications')
      if (!response.ok) throw new Error('Failed to fetch applications')
      const data = await response.json()
      setApplications(data || [])
    } catch (err) {
      setError(err.message)
    }
  }

  // Fetch config
  const fetchConfig = async () => {
    try {
      const response = await fetch('/api/config')
      if (!response.ok) throw new Error('Failed to fetch config')
      const data = await response.json()
      setConfig(data)
    } catch (err) {
      setError(err.message)
    }
  }

  // Fetch current window
  const fetchCurrentWindow = async () => {
    try {
      const response = await fetch('/api/window/current')
      if (response.ok) {
        const data = await response.json()
        setCurrentWindow(data)
      }
    } catch (err) {
      console.error('Failed to fetch current window:', err)
    }
  }

  // Initial load
  useEffect(() => {
    const loadData = async () => {
      setLoading(true)
      await Promise.all([
        fetchApplications(),
        fetchConfig(),
        fetchCurrentWindow()
      ])
      setLoading(false)
    }
    loadData()

    // Poll for updates
    const interval = setInterval(() => {
      fetchApplications()
      fetchCurrentWindow()
    }, 2000)

    return () => clearInterval(interval)
  }, [])

  // Toggle allowlist
  const toggleAllowlist = async (appClass, isAllowlisted) => {
    try {
      if (isAllowlisted) {
        await fetch(`/api/applications/allowlist/${appClass}`, {
          method: 'DELETE'
        })
      } else {
        await fetch('/api/applications/allowlist', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ app_class: appClass })
        })
      }
      await fetchApplications()
    } catch (err) {
      setError(err.message)
    }
  }

  // Add pattern
  const addPattern = async (pattern) => {
    try {
      await fetch('/api/config/patterns', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pattern })
      })
      await fetchConfig()
    } catch (err) {
      setError(err.message)
    }
  }

  // Remove pattern
  const removePattern = async (pattern) => {
    try {
      await fetch('/api/config/patterns', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pattern })
      })
      await fetchConfig()
    } catch (err) {
      setError(err.message)
    }
  }

  if (loading) {
    return (
      <div className="app">
        <div className="loading">Loading...</div>
      </div>
    )
  }

  return (
    <div className="app">
      <header className="header">
        <h1>🎯 FocusStreamer</h1>
        <p className="subtitle">Virtual Display for Discord Screen Sharing</p>
      </header>

      {error && (
        <div className="error-banner">
          <strong>Error:</strong> {error}
          <button onClick={() => setError(null)}>×</button>
        </div>
      )}

      <div className="container">
        <div className="main-content">
          <section className="section">
            <CurrentWindow window={currentWindow} />
          </section>

          <section className="section">
            <h2>Applications</h2>
            <p className="section-description">
              Select which applications can appear on your virtual display when they're focused.
            </p>
            <ApplicationList
              applications={applications}
              onToggleAllowlist={toggleAllowlist}
            />
          </section>

          <section className="section">
            <h2>Pattern Matching</h2>
            <p className="section-description">
              Use regex patterns to automatically allowlist applications.
            </p>
            <PatternManager
              patterns={config?.allowlist_patterns || []}
              onAddPattern={addPattern}
              onRemovePattern={removePattern}
            />
          </section>
        </div>

        <aside className="sidebar">
          <div className="info-card">
            <h3>How to Use</h3>
            <ol>
              <li>Select applications you want to share</li>
              <li>Or add regex patterns for auto-allowlisting</li>
              <li>Start Discord screen share</li>
              <li>Share the FocusStreamer window</li>
              <li>Only allowlisted focused windows will appear</li>
            </ol>
          </div>

          <div className="info-card">
            <h3>Status</h3>
            <div className="status-item">
              <span className="status-label">Applications:</span>
              <span className="status-value">{applications.length}</span>
            </div>
            <div className="status-item">
              <span className="status-label">Allowlisted:</span>
              <span className="status-value">
                {applications.filter(a => a.allowlisted).length}
              </span>
            </div>
            <div className="status-item">
              <span className="status-label">Patterns:</span>
              <span className="status-value">
                {config?.allowlist_patterns?.length || 0}
              </span>
            </div>
          </div>
        </aside>
      </div>
    </div>
  )
}

export default App
