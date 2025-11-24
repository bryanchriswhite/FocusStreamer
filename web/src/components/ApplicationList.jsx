import './ApplicationList.css'

function ApplicationList({ applications, onToggleAllowlist, selectedApp, onSelectApp }) {
  if (!applications || applications.length === 0) {
    return (
      <div className="empty-state">
        <p>No applications detected</p>
        <p className="hint">Make sure you have some windows open</p>
      </div>
    )
  }

  return (
    <div className="application-list">
      {applications.map((app) => (
        <div
          key={app.id}
          className={`application-item ${app.allowlisted ? 'allowlisted' : ''} ${selectedApp?.id === app.id ? 'selected' : ''}`}
          onClick={() => onSelectApp && onSelectApp(app)}
        >
          <div className="app-info">
            <div className="app-header">
              <div className="app-name">{app.name}</div>
              {app.allowlisted && <span className="badge">Allowlisted</span>}
            </div>
            <div className="app-details">
              <div className="app-class">
                <strong>Class:</strong> {app.window_class}
              </div>
              <div className="app-pid">
                <strong>PID:</strong> {app.pid}
              </div>
            </div>
          </div>
          <button
            className={app.allowlisted ? 'danger' : 'primary'}
            onClick={(e) => {
              e.stopPropagation()
              onToggleAllowlist(app.window_class, app.allowlisted)
            }}
          >
            {app.allowlisted ? 'Remove' : 'Add'}
          </button>
        </div>
      ))}
    </div>
  )
}

export default ApplicationList
