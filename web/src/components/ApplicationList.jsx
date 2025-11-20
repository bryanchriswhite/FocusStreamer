import './ApplicationList.css'

function ApplicationList({ applications, onToggleWhitelist }) {
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
          className={`application-item ${app.whitelisted ? 'whitelisted' : ''}`}
        >
          <div className="app-info">
            <div className="app-name">{app.name}</div>
            <div className="app-class">{app.window_class}</div>
          </div>
          <button
            className={app.whitelisted ? 'danger' : 'primary'}
            onClick={() => onToggleWhitelist(app.window_class, app.whitelisted)}
          >
            {app.whitelisted ? 'Remove' : 'Add'}
          </button>
        </div>
      ))}
    </div>
  )
}

export default ApplicationList
