import { useState, useEffect } from 'react'
import './App.css'
import ApplicationList from './components/ApplicationList'
import ApplicationPreview from './components/ApplicationPreview'
import CurrentWindow from './components/CurrentWindow'
import PatternManager from './components/PatternManager'
import PlaceholderUpload from './components/PlaceholderUpload'
import ProfileSelector from './components/ProfileSelector'

function App() {
  const [applications, setApplications] = useState([])
  const [currentWindow, setCurrentWindow] = useState(null)
  const [config, setConfig] = useState(null)
  const [selectedApp, setSelectedApp] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [placeholderImages, setPlaceholderImages] = useState([])
  const [profiles, setProfiles] = useState([])
  const [activeProfileId, setActiveProfileId] = useState('default')

  // Fetch applications
  const fetchApplications = async () => {
    try {
      const response = await fetch('/api/applications')
      if (!response.ok) throw new Error('Failed to fetch applications')
      const data = await response.json()

      // Sort applications for stable ordering
      const sorted = (data || []).sort((a, b) => {
        // First by allowlisted status (allowlisted first)
        if (a.allowlisted !== b.allowlisted) {
          return a.allowlisted ? -1 : 1
        }
        // Then alphabetically by name
        return a.name.localeCompare(b.name)
      })

      setApplications(sorted)
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

  // Fetch placeholder images
  const fetchPlaceholderImages = async () => {
    try {
      const response = await fetch('/api/config/placeholder-images')
      if (response.ok) {
        const data = await response.json()
        setPlaceholderImages(data.images || [])
      }
    } catch (err) {
      console.error('Failed to fetch placeholder images:', err)
    }
  }

  // Fetch profiles
  const fetchProfiles = async () => {
    try {
      const response = await fetch('/api/profiles')
      if (response.ok) {
        const data = await response.json()
        setProfiles(data.profiles || [])
        setActiveProfileId(data.active_profile_id || 'default')
      }
    } catch (err) {
      console.error('Failed to fetch profiles:', err)
    }
  }

  // Switch profile
  const switchProfile = async (profileId) => {
    try {
      const response = await fetch('/api/profiles/active', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ profile_id: profileId })
      })
      if (response.ok) {
        setActiveProfileId(profileId)
        // Refresh all data for the new profile
        await Promise.all([
          fetchApplications(),
          fetchConfig(),
          fetchPlaceholderImages()
        ])
      }
    } catch (err) {
      setError(err.message)
    }
  }

  // Create profile
  const createProfile = async (name) => {
    try {
      const response = await fetch('/api/profiles', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name })
      })
      if (response.ok) {
        await fetchProfiles()
      }
    } catch (err) {
      setError(err.message)
    }
  }

  // Delete profile
  const deleteProfile = async (profileId) => {
    try {
      const response = await fetch(`/api/profiles/${encodeURIComponent(profileId)}`, {
        method: 'DELETE'
      })
      if (response.ok) {
        await fetchProfiles()
        // If we deleted the active profile, refresh everything
        if (profileId === activeProfileId) {
          await Promise.all([
            fetchApplications(),
            fetchConfig(),
            fetchPlaceholderImages()
          ])
        }
      }
    } catch (err) {
      setError(err.message)
    }
  }

  // Duplicate profile
  const duplicateProfile = async (profileId, newName) => {
    try {
      const response = await fetch(`/api/profiles/${encodeURIComponent(profileId)}/duplicate`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName })
      })
      if (response.ok) {
        await fetchProfiles()
      }
    } catch (err) {
      setError(err.message)
    }
  }

  // Rename profile
  const renameProfile = async (profileId, newName) => {
    try {
      // First get the current profile data
      const getResponse = await fetch(`/api/profiles/${encodeURIComponent(profileId)}`)
      if (!getResponse.ok) {
        throw new Error('Failed to get profile')
      }
      const profile = await getResponse.json()

      // Update with new name
      profile.name = newName
      const response = await fetch(`/api/profiles/${encodeURIComponent(profileId)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(profile)
      })
      if (response.ok) {
        await fetchProfiles()
      }
    } catch (err) {
      setError(err.message)
    }
  }

  // Initial load
  useEffect(() => {
    const loadData = async () => {
      setLoading(true)
      await Promise.all([
        fetchProfiles(),
        fetchApplications(),
        fetchConfig(),
        fetchCurrentWindow(),
        fetchPlaceholderImages()
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
        // URL encode the appClass to handle special characters like dots
        await fetch(`/api/applications/allowlist/${encodeURIComponent(appClass)}`, {
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

  // Upload placeholder image
  const uploadPlaceholder = async (file) => {
    const formData = new FormData()
    formData.append('image', file)

    const response = await fetch('/api/config/placeholder-images', {
      method: 'POST',
      body: formData
    })

    if (!response.ok) {
      const text = await response.text()
      throw new Error(text || 'Upload failed')
    }

    await fetchPlaceholderImages()
  }

  // Delete placeholder image by ID
  const deletePlaceholder = async (id) => {
    const response = await fetch(`/api/config/placeholder-images/${encodeURIComponent(id)}`, {
      method: 'DELETE'
    })

    if (!response.ok) {
      const text = await response.text()
      throw new Error(text || 'Delete failed')
    }

    await fetchPlaceholderImages()
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
        <div className="header-main">
          <h1>FocusStreamer</h1>
          <p className="subtitle">Virtual Display for Discord Screen Sharing</p>
        </div>
        <div className="header-right">
          <ProfileSelector
            profiles={profiles}
            activeProfileId={activeProfileId}
            onSwitchProfile={switchProfile}
            onCreateProfile={createProfile}
            onDeleteProfile={deleteProfile}
            onDuplicateProfile={duplicateProfile}
            onRenameProfile={renameProfile}
          />
          <nav className="header-nav">
            <a href="/" className="nav-link">Stream</a>
            <a href="/control" className="nav-link">Control</a>
          </nav>
        </div>
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
            <h2>Selected Application</h2>
            <ApplicationPreview application={selectedApp} />
          </section>

          <section className="section">
            <h2>Applications</h2>
            <p className="section-description">
              Click an application to view details. Add to allowlist to show it on your virtual display when focused.
            </p>
            <ApplicationList
              applications={applications}
              onToggleAllowlist={toggleAllowlist}
              selectedApp={selectedApp}
              onSelectApp={setSelectedApp}
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

          <section className="section">
            <h2>Waiting Screen</h2>
            <PlaceholderUpload
              images={placeholderImages}
              onUpload={uploadPlaceholder}
              onDelete={deletePlaceholder}
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
