import { useState } from 'react'
import './PatternManager.css'

// Helper to check if pattern has case-insensitive flag
const isCaseInsensitive = (pattern) => pattern.startsWith('(?i)')

// Helper to get display pattern (without flag prefix)
const getDisplayPattern = (pattern) => {
  if (pattern.startsWith('(?i)')) {
    return pattern.substring(4)
  }
  return pattern
}

// Helper to add/remove case-insensitive flag
const toggleCaseFlag = (pattern, shouldBeInsensitive) => {
  const hasFlag = pattern.startsWith('(?i)')
  if (shouldBeInsensitive && !hasFlag) {
    return '(?i)' + pattern
  } else if (!shouldBeInsensitive && hasFlag) {
    return pattern.substring(4)
  }
  return pattern
}

function PatternManager({ patterns, onAddPattern, onRemovePattern }) {
  const [newPattern, setNewPattern] = useState('')
  const [newCaseInsensitive, setNewCaseInsensitive] = useState(false)
  const [error, setError] = useState('')
  const [editingIndex, setEditingIndex] = useState(null)
  const [editValue, setEditValue] = useState('')
  const [editCaseInsensitive, setEditCaseInsensitive] = useState(false)
  const [editError, setEditError] = useState('')

  const handleAdd = () => {
    if (!newPattern.trim()) {
      setError('Pattern cannot be empty')
      return
    }

    const patternToAdd = newCaseInsensitive ? '(?i)' + newPattern : newPattern

    try {
      // Test if it's a valid regex
      new RegExp(patternToAdd)
      onAddPattern(patternToAdd)
      setNewPattern('')
      setNewCaseInsensitive(false)
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

  const startEdit = (index, pattern) => {
    setEditingIndex(index)
    setEditValue(getDisplayPattern(pattern))
    setEditCaseInsensitive(isCaseInsensitive(pattern))
    setEditError('')
  }

  const cancelEdit = () => {
    setEditingIndex(null)
    setEditValue('')
    setEditCaseInsensitive(false)
    setEditError('')
  }

  const saveEdit = async (oldPattern) => {
    if (!editValue.trim()) {
      setEditError('Pattern cannot be empty')
      return
    }

    const newPatternValue = editCaseInsensitive ? '(?i)' + editValue : editValue

    try {
      // Test if it's a valid regex
      new RegExp(newPatternValue)
    } catch (err) {
      setEditError('Invalid regex pattern')
      return
    }

    // If pattern unchanged, just cancel edit
    if (newPatternValue === oldPattern) {
      cancelEdit()
      return
    }

    // Remove old pattern and add new one
    await onRemovePattern(oldPattern)
    await onAddPattern(newPatternValue)
    cancelEdit()
  }

  const handleEditKeyPress = (e, oldPattern) => {
    if (e.key === 'Enter') {
      saveEdit(oldPattern)
    } else if (e.key === 'Escape') {
      cancelEdit()
    }
  }

  const togglePatternCase = async (pattern) => {
    const isCurrentlyInsensitive = isCaseInsensitive(pattern)
    const newPattern = toggleCaseFlag(pattern, !isCurrentlyInsensitive)
    await onRemovePattern(pattern)
    await onAddPattern(newPattern)
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
      <div className="add-options">
        <label>
          <input
            type="checkbox"
            checked={newCaseInsensitive}
            onChange={(e) => setNewCaseInsensitive(e.target.checked)}
          />
          Case insensitive
        </label>
      </div>

      {error && <div className="pattern-error">{error}</div>}

      {patterns && patterns.length > 0 ? (
        <div className="pattern-list">
          {patterns.map((pattern, index) => (
            <div key={index} className="pattern-item">
              {editingIndex === index ? (
                <>
                  <div className="pattern-info">
                    <input
                      type="text"
                      className="edit-input"
                      value={editValue}
                      onChange={(e) => {
                        setEditValue(e.target.value)
                        setEditError('')
                      }}
                      onKeyDown={(e) => handleEditKeyPress(e, pattern)}
                      autoFocus
                    />
                    <label className="case-toggle">
                      <input
                        type="checkbox"
                        checked={editCaseInsensitive}
                        onChange={(e) => setEditCaseInsensitive(e.target.checked)}
                      />
                      Case insensitive
                    </label>
                  </div>
                  <div className="pattern-actions">
                    <button
                      className="primary small"
                      onClick={() => saveEdit(pattern)}
                    >
                      Save
                    </button>
                    <button
                      className="secondary small"
                      onClick={cancelEdit}
                    >
                      Cancel
                    </button>
                  </div>
                </>
              ) : (
                <>
                  <div className="pattern-info">
                    <code
                      className="clickable"
                      onClick={() => startEdit(index, pattern)}
                      title="Click to edit"
                    >
                      {getDisplayPattern(pattern)}
                    </code>
                    <div className="pattern-flags">
                      <label className={`case-toggle ${isCaseInsensitive(pattern) ? 'active' : ''}`}>
                        <input
                          type="checkbox"
                          checked={isCaseInsensitive(pattern)}
                          onChange={() => togglePatternCase(pattern)}
                        />
                        Case insensitive
                      </label>
                    </div>
                  </div>
                  <div className="pattern-actions">
                    <button
                      className="danger small"
                      onClick={() => onRemovePattern(pattern)}
                    >
                      Remove
                    </button>
                  </div>
                </>
              )}
            </div>
          ))}
          {editError && <div className="pattern-error">{editError}</div>}
        </div>
      ) : (
        <div className="empty-patterns">
          <p>No patterns configured</p>
          <p className="hint">Add regex patterns to auto-allowlist applications</p>
        </div>
      )}
    </div>
  )
}

export default PatternManager
