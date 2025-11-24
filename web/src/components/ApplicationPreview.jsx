import { useState, useEffect } from 'react'
import './ApplicationPreview.css'

function ApplicationPreview({ application }) {
  const [screenshot, setScreenshot] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  useEffect(() => {
    if (!application) {
      setScreenshot(null)
      setError(null)
      return
    }

    // Fetch screenshot
    const fetchScreenshot = async () => {
      setLoading(true)
      setError(null)

      try {
        const response = await fetch(`/api/window/${application.window_class}/screenshot`)

        if (response.ok) {
          const blob = await response.blob()
          const url = URL.createObjectURL(blob)
          setScreenshot(url)
        } else if (response.status === 404) {
          setError('Window not currently visible')
        } else {
          setError('Failed to load screenshot')
        }
      } catch (err) {
        setError('Failed to load screenshot')
        console.error('Screenshot error:', err)
      } finally {
        setLoading(false)
      }
    }

    fetchScreenshot()

    // Cleanup blob URL when component unmounts or application changes
    return () => {
      if (screenshot) {
        URL.revokeObjectURL(screenshot)
      }
    }
  }, [application?.window_class])

  if (!application) {
    return (
      <div className="preview-empty">
        <div className="preview-placeholder">
          <p>Select an application to view details</p>
          <p className="hint">Click on an application from the list</p>
        </div>
      </div>
    )
  }

  return (
    <div className="application-preview">
      <div className="preview-header">
        <h3>{application.name}</h3>
        {application.allowlisted && (
          <span className="status-badge allowlisted">Allowlisted</span>
        )}
      </div>

      {/* Screenshot Preview */}
      <div className="preview-screenshot">
        {loading && (
          <div className="screenshot-loading">
            <p>Loading screenshot...</p>
          </div>
        )}

        {error && (
          <div className="screenshot-error">
            <p>⚠️ {error}</p>
          </div>
        )}

        {screenshot && !loading && !error && (
          <img src={screenshot} alt={`Screenshot of ${application.name}`} />
        )}
      </div>

      <div className="preview-details">
        <div className="detail-row">
          <span className="detail-label">Window Class:</span>
          <span className="detail-value monospace">{application.window_class}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Process ID:</span>
          <span className="detail-value">{application.pid}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">ID:</span>
          <span className="detail-value monospace">{application.id}</span>
        </div>
      </div>

      <div className="preview-note">
        <p className="hint">
          This preview shows the current window content. The virtual display will show this application when it's focused and allowlisted.
        </p>
      </div>
    </div>
  )
}

export default ApplicationPreview
