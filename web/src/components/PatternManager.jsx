import { useState } from 'react'
import './PatternManager.css'

function PatternManager({ patterns, onAddPattern, onRemovePattern }) {
  const [newPattern, setNewPattern] = useState('')
  const [error, setError] = useState('')

  const handleAdd = () => {
    if (!newPattern.trim()) {
      setError('Pattern cannot be empty')
      return
    }

    try {
      // Test if it's a valid regex
      new RegExp(newPattern)
      onAddPattern(newPattern)
      setNewPattern('')
      setError('')
    } catch (err) {
      setError('Invalid regex pattern')
    }
  }

  const handleKeyPress = (e) => {
    if (e.key === 'Enter') {
      handleAdd()
    }
  }

  return (
    <div className="pattern-manager">
      <div className="pattern-input">
        <input
          type="text"
          placeholder="Enter regex pattern (e.g., .*Terminal.*)"
          value={newPattern}
          onChange={(e) => {
            setNewPattern(e.target.value)
            setError('')
          }}
          onKeyPress={handleKeyPress}
        />
        <button className="primary" onClick={handleAdd}>
          Add Pattern
        </button>
      </div>

      {error && <div className="pattern-error">{error}</div>}

      {patterns && patterns.length > 0 ? (
        <div className="pattern-list">
          {patterns.map((pattern, index) => (
            <div key={index} className="pattern-item">
              <code>{pattern}</code>
              <button
                className="danger"
                onClick={() => onRemovePattern(pattern)}
              >
                Remove
              </button>
            </div>
          ))}
        </div>
      ) : (
        <div className="empty-patterns">
          <p>No patterns configured</p>
          <p className="hint">Add regex patterns to auto-whitelist applications</p>
        </div>
      )}
    </div>
  )
}

export default PatternManager
