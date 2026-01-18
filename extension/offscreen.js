const HEARTBEAT_MS = 2000

const sendHeartbeat = () => {
  chrome.runtime.sendMessage({ type: 'heartbeat' })
}

sendHeartbeat()
setInterval(sendHeartbeat, HEARTBEAT_MS)
