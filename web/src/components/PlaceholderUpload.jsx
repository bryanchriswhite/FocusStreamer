import { useState, useRef } from 'react'
import './PlaceholderUpload.css'

function PlaceholderUpload({ currentPath, onUpload, onDelete }) {
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState(null)
  const [dragActive, setDragActive] = useState(false)
  const fileInputRef = useRef(null)

  const handleFileSelect = async (file) => {
    if (!file) return

    // Validate file type
    const validTypes = ['image/png', 'image/jpeg', 'image/gif']
    if (!validTypes.includes(file.type)) {
      setError('Invalid file type. Please select PNG, JPEG, or GIF.')
      return
    }

    // Validate file size (10MB max)
    if (file.size > 10 * 1024 * 1024) {
      setError('File too large. Maximum size is 10MB.')
      return
    }

    setError(null)
    setUploading(true)

    try {
      await onUpload(file)
    } catch (err) {
      setError(err.message || 'Upload failed')
    } finally {
      setUploading(false)
    }
  }

  const handleInputChange = (e) => {
    const file = e.target.files?.[0]
    handleFileSelect(file)
    // Reset input so the same file can be selected again
    e.target.value = ''
  }

  const handleDrag = (e) => {
    e.preventDefault()
    e.stopPropagation()
    if (e.type === 'dragenter' || e.type === 'dragover') {
      setDragActive(true)
    } else if (e.type === 'dragleave') {
      setDragActive(false)
    }
  }

  const handleDrop = (e) => {
    e.preventDefault()
    e.stopPropagation()
    setDragActive(false)

    const file = e.dataTransfer.files?.[0]
    handleFileSelect(file)
  }

  const handleDelete = async () => {
    setError(null)
    try {
      await onDelete()
    } catch (err) {
      setError(err.message || 'Delete failed')
    }
  }

  const triggerFileSelect = () => {
    fileInputRef.current?.click()
  }

  return (
    <div className="placeholder-upload">
      <p className="description">
        Set a custom image to display when no allowlisted window is focused.
      </p>

      {error && (
        <div className="upload-error">
          {error}
          <button className="dismiss-error" onClick={() => setError(null)}>×</button>
        </div>
      )}

      {currentPath ? (
        <div className="current-placeholder">
          <div className="preview-container">
            <img
              src={`/api/config/placeholder-image?t=${Date.now()}`}
              alt="Current placeholder"
              className="preview-image"
            />
          </div>
          <div className="placeholder-actions">
            <button
              className="btn btn-secondary"
              onClick={triggerFileSelect}
              disabled={uploading}
            >
              {uploading ? 'Uploading...' : 'Change Image'}
            </button>
            <button
              className="btn btn-danger"
              onClick={handleDelete}
              disabled={uploading}
            >
              Remove
            </button>
          </div>
        </div>
      ) : (
        <div
          className={`drop-zone ${dragActive ? 'drag-active' : ''}`}
          onDragEnter={handleDrag}
          onDragLeave={handleDrag}
          onDragOver={handleDrag}
          onDrop={handleDrop}
          onClick={triggerFileSelect}
        >
          {uploading ? (
            <span className="uploading-text">Uploading...</span>
          ) : (
            <>
              <span className="drop-icon">📷</span>
              <span className="drop-text">
                Drag & drop an image here, or click to select
              </span>
              <span className="drop-hint">PNG, JPEG, or GIF (max 10MB)</span>
            </>
          )}
        </div>
      )}

      <input
        ref={fileInputRef}
        type="file"
        accept="image/png,image/jpeg,image/gif"
        onChange={handleInputChange}
        className="file-input-hidden"
      />
    </div>
  )
}

export default PlaceholderUpload
