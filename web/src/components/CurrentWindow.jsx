import './CurrentWindow.css'

function CurrentWindow({ window }) {
  if (!window) {
    return (
      <div className="current-window">
        <h2>Current Window</h2>
        <div className="no-window">
          <p>No window is currently focused</p>
        </div>
      </div>
    )
  }

  return (
    <div className="current-window">
      <h2>Current Window</h2>
      <div className="window-card">
        <div className="window-header">
          <span className="window-indicator"></span>
          <span className="window-title">{window.title || 'Untitled'}</span>
        </div>
        <div className="window-details">
          <div className="detail-row">
            <span className="label">Class:</span>
            <span className="value">{window.class || 'Unknown'}</span>
          </div>
          <div className="detail-row">
            <span className="label">PID:</span>
            <span className="value">{window.pid || 'N/A'}</span>
          </div>
          <div className="detail-row">
            <span className="label">Size:</span>
            <span className="value">
              {window.geometry?.width || 0} × {window.geometry?.height || 0}
            </span>
          </div>
          <div className="detail-row">
            <span className="label">Position:</span>
            <span className="value">
              ({window.geometry?.x || 0}, {window.geometry?.y || 0})
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

export default CurrentWindow
