import { useState } from 'react'
import './ProfileSelector.css'

function ProfileSelector({
  profiles,
  activeProfileId,
  onSwitchProfile,
  onCreateProfile,
  onDeleteProfile,
  onDuplicateProfile,
  onRenameProfile
}) {
  const [isCreating, setIsCreating] = useState(false)
  const [newProfileName, setNewProfileName] = useState('')
  const [showMenu, setShowMenu] = useState(false)
  const [menuProfileId, setMenuProfileId] = useState(null)
  const [isRenaming, setIsRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const [isDuplicating, setIsDuplicating] = useState(false)
  const [duplicateName, setDuplicateName] = useState('')

  const activeProfile = profiles.find(p => p.id === activeProfileId)

  const handleCreateSubmit = async (e) => {
    e.preventDefault()
    if (newProfileName.trim()) {
      await onCreateProfile(newProfileName.trim())
      setNewProfileName('')
      setIsCreating(false)
    }
  }

  const handleProfileClick = (profileId) => {
    if (profileId !== activeProfileId) {
      onSwitchProfile(profileId)
    }
    setShowMenu(false)
    setMenuProfileId(null)
  }

  const handleMenuClick = (e, profileId) => {
    e.stopPropagation()
    if (menuProfileId === profileId) {
      setMenuProfileId(null)
    } else {
      setMenuProfileId(profileId)
    }
  }

  const handleDeleteClick = async (e, profileId) => {
    e.stopPropagation()
    if (profileId === 'default') {
      alert('Cannot delete the default profile')
      return
    }
    if (confirm('Are you sure you want to delete this profile?')) {
      await onDeleteProfile(profileId)
    }
    setMenuProfileId(null)
  }

  const handleRenameStart = (e, profile) => {
    e.stopPropagation()
    setIsRenaming(true)
    setRenameValue(profile.name)
    setMenuProfileId(profile.id)
  }

  const handleRenameSubmit = async (e) => {
    e.preventDefault()
    if (renameValue.trim() && menuProfileId) {
      await onRenameProfile(menuProfileId, renameValue.trim())
      setIsRenaming(false)
      setRenameValue('')
      setMenuProfileId(null)
    }
  }

  const handleDuplicateStart = (e, profile) => {
    e.stopPropagation()
    setIsDuplicating(true)
    setDuplicateName(profile.name + ' Copy')
    setMenuProfileId(profile.id)
  }

  const handleDuplicateSubmit = async (e) => {
    e.preventDefault()
    if (duplicateName.trim() && menuProfileId) {
      await onDuplicateProfile(menuProfileId, duplicateName.trim())
      setIsDuplicating(false)
      setDuplicateName('')
      setMenuProfileId(null)
    }
  }

  const handleCancelAction = () => {
    setIsRenaming(false)
    setIsDuplicating(false)
    setMenuProfileId(null)
  }

  return (
    <div className="profile-selector">
      <div className="profile-selector-current" onClick={() => setShowMenu(!showMenu)}>
        <span className="profile-icon">&#128100;</span>
        <span className="profile-name">{activeProfile?.name || 'Default'}</span>
        <span className="profile-dropdown-arrow">{showMenu ? '\u25B2' : '\u25BC'}</span>
      </div>

      {showMenu && (
        <div className="profile-dropdown">
          <div className="profile-list">
            {profiles.map(profile => (
              <div
                key={profile.id}
                className={`profile-item ${profile.id === activeProfileId ? 'active' : ''}`}
                onClick={() => handleProfileClick(profile.id)}
              >
                <span className="profile-item-name">{profile.name}</span>
                {profile.id === activeProfileId && <span className="profile-check">&#10003;</span>}
                <button
                  className="profile-menu-btn"
                  onClick={(e) => handleMenuClick(e, profile.id)}
                >
                  &#8942;
                </button>

                {menuProfileId === profile.id && !isRenaming && !isDuplicating && (
                  <div className="profile-menu">
                    <button onClick={(e) => handleRenameStart(e, profile)}>Rename</button>
                    <button onClick={(e) => handleDuplicateStart(e, profile)}>Duplicate</button>
                    {profile.id !== 'default' && (
                      <button
                        className="danger"
                        onClick={(e) => handleDeleteClick(e, profile.id)}
                      >
                        Delete
                      </button>
                    )}
                  </div>
                )}

                {menuProfileId === profile.id && isRenaming && (
                  <form className="profile-inline-form" onSubmit={handleRenameSubmit} onClick={e => e.stopPropagation()}>
                    <input
                      type="text"
                      value={renameValue}
                      onChange={(e) => setRenameValue(e.target.value)}
                      placeholder="Profile name"
                      autoFocus
                    />
                    <button type="submit">Save</button>
                    <button type="button" onClick={handleCancelAction}>Cancel</button>
                  </form>
                )}

                {menuProfileId === profile.id && isDuplicating && (
                  <form className="profile-inline-form" onSubmit={handleDuplicateSubmit} onClick={e => e.stopPropagation()}>
                    <input
                      type="text"
                      value={duplicateName}
                      onChange={(e) => setDuplicateName(e.target.value)}
                      placeholder="New profile name"
                      autoFocus
                    />
                    <button type="submit">Create</button>
                    <button type="button" onClick={handleCancelAction}>Cancel</button>
                  </form>
                )}
              </div>
            ))}
          </div>

          <div className="profile-actions">
            {isCreating ? (
              <form className="profile-create-form" onSubmit={handleCreateSubmit}>
                <input
                  type="text"
                  value={newProfileName}
                  onChange={(e) => setNewProfileName(e.target.value)}
                  placeholder="New profile name"
                  autoFocus
                />
                <div className="profile-create-buttons">
                  <button type="submit">Create</button>
                  <button type="button" onClick={() => setIsCreating(false)}>Cancel</button>
                </div>
              </form>
            ) : (
              <button
                className="profile-new-btn"
                onClick={() => setIsCreating(true)}
              >
                + New Profile
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export default ProfileSelector
