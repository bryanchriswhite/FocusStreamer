import { useState, useRef } from 'react'
import './PlaceholderUpload.css'

function PlaceholderUpload({ images, onUpload, onDelete }) {
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
    const files = e.target.files
    if (files) {
      // Handle multiple files
      Array.from(files).forEach(file => handleFileSelect(file))
    }
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

    const files = e.dataTransfer.files
    if (files) {
      Array.from(files).forEach(file => handleFileSelect(file))
    }
  }

  const handleDelete = async (id) => {
    setError(null)
    try {
      await onDelete(id)
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
        Set custom images to display when no allowlisted window is focused.
        Images will cycle in order on each standby transition.
      </p>

      {error && (
        <div className="upload-error">
          {error}
          <button className="dismiss-error" onClick={() => setError(null)}>×</button>
        </div>
      )}

      <div className="placeholder-grid">
        {/* Display existing images */}
        {images.map((img) => (
          <div key={img.id} className="placeholder-item">
            <img
              src={`/api/config/placeholder-images/${img.id}?t=${Date.now()}`}
              alt="Placeholder"
              className="placeholder-preview"
            />
            <button
              className="delete-btn"
              onClick={() => handleDelete(img.id)}
              title="Remove image"
            >
              ×
            </button>
          </div>
        ))}

        {/* Add new image tile */}
        <div
          className={`placeholder-item add-new ${dragActive ? 'drag-active' : ''}`}
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
              <span className="add-icon">+</span>
              <span className="add-text">Add Image</span>
            </>
          )}
        </div>
      </div>

      <input
        ref={fileInputRef}
        type="file"
        accept="image/png,image/jpeg,image/gif"
        multiple
        onChange={handleInputChange}
        className="file-input-hidden"
      />
    </div>
  )
}

export default PlaceholderUpload
