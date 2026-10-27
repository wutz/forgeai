import {useCallback, useEffect, useState} from 'react'
import {ImageBuildLogs} from '../wailsjs/go/app/App'
import {errText, everyWhileVisible} from './util'
import {t} from './i18n'
import {Dialog, LogView} from './ui'

// LOG_LINES is how many of the last lines of a build's pod the dialog reads.
const LOG_LINES = 500
// LOG_POLL_MS is how often the log is read again while the build is still running.
const LOG_POLL_MS = 5000

// ImageBuildLogsDialog shows the last lines of the build pod. While the build is
// Pending or Running the log is read again every few seconds.
export function ImageBuildLogsDialog({name, running, onClose}: {
  name: string, running: boolean, onClose: () => void
}) {
  const [text, setText] = useState<string | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => ImageBuildLogs(name, LOG_LINES).then(s => {
    setText(s)
    setError('')
  }, e => setError(errText(e))), [name])

  useEffect(() => {
    load()
    if (!running) return
    return everyWhileVisible(() => { load() }, LOG_POLL_MS)
  }, [load, running])

  return (
    <Dialog title={t('builds.logsTitle', {name})} onClose={onClose} width={900}>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <LogView text={text} onRefresh={load}/>
    </Dialog>
  )
}
