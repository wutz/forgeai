import {useEffect, useRef, useState} from 'react'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import {
  CloseSandboxTerminal, OpenSandboxTerminal, ResizeSandboxTerminal, SandboxTerminalInput,
} from '../wailsjs/go/app/App'
import {EventsOn} from '../wailsjs/runtime/runtime'
import {errText} from './util'
import {Icon} from './ui'
import {t} from './i18n'

// SandboxTerminal is an interactive shell in a running sandbox. The session
// lives as long as the component: leaving the tab closes the shell. When the
// shell ends or fails to open, Reconnect starts a new one.
export function SandboxTerminal({name}: {name: string}) {
  const host = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const [maximized, setMaximized] = useState(false)
  const [ended, setEnded] = useState(false)
  // attempt changes to start a new shell.
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const el = host.current
    if (!el) return
    setError('')
    setEnded(false)
    // The id is chosen here so the output listeners exist before the shell
    // prints its first prompt.
    const id = crypto.randomUUID()
    const term = new Terminal({
      cursorBlink: true,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      fontSize: 12,
      lineHeight: 1.25,
      scrollback: 5000,
      theme: {background: '#0f1117', foreground: '#e4e4e7', cursor: '#a5b4fc', selectionBackground: '#3730a3'},
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    fit.fit()

    const offData = EventsOn(`sandbox-terminal:${id}`, (data: string) => term.write(data))
    const offExit = EventsOn(`sandbox-terminal:${id}:exit`, (msg: string) => {
      const text = msg ? t('terminal.sessionEndedWith', {message: msg}) : t('terminal.sessionEnded')
      term.write(`\r\n\x1b[90m[${text}]\x1b[0m\r\n`)
      setEnded(true)
    })
    const onData = term.onData(data => SandboxTerminalInput(id, data).catch(() => {}))
    const ro = new ResizeObserver(() => {
      // A hidden view has no size; fitting it would resize the shell to a
      // couple of columns until the view is shown again.
      if (!el.offsetWidth || !el.offsetHeight) return
      fit.fit()
      ResizeSandboxTerminal(id, term.cols, term.rows).catch(() => {})
    })
    ro.observe(el)

    // closed is set when the view goes away. If the shell opens after that,
    // the cleanup's close found no session yet, so this one closes it.
    let closed = false
    OpenSandboxTerminal(name, id, term.cols, term.rows).then(() => {
      if (closed) CloseSandboxTerminal(id).catch(() => {})
      else term.focus()
    }, e => {
      if (!closed) setError(errText(e))
    })

    return () => {
      closed = true
      ro.disconnect()
      onData.dispose()
      offData()
      offExit()
      CloseSandboxTerminal(id).catch(() => {})
      term.dispose()
    }
  }, [name, attempt])

  // Maximizing only changes the layout; the resize observer refits the
  // terminal and sends the new size to the shell.
  return (
    <div className={`term-wrap${maximized ? ' term-max' : ''}`}>
      <div className="term-bar">
        {(ended || error) && <button className="btn btn-sm btn-accent" onClick={() => setAttempt(a => a + 1)}>
          <Icon name="refresh" size={13}/>{t('terminal.reconnect')}
        </button>}
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={() => setMaximized(m => !m)}
                aria-label={maximized ? t('terminal.restoreLabel') : t('terminal.maximizeLabel')}>
          <Icon name={maximized ? 'minimize' : 'maximize'} size={13}/>
          {maximized ? t('terminal.restore') : t('terminal.maximize')}
        </button>
      </div>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <div className="term" ref={host}/>
    </div>
  )
}
