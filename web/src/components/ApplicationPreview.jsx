import './ApplicationPreview.css'

function ApplicationPreview({ application }) {
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
        <p><strong>Note:</strong> Window preview/screenshot functionality coming soon!</p>
        <p className="hint">
          The virtual display will show this application when it's focused and allowlisted.
        </p>
      </div>
    </div>
  )
}

export default ApplicationPreview
