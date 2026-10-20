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

// SandboxTerminal is an interactive shell in a running sandbox. The session
// lives as long as the component: leaving the tab closes the shell.
export function SandboxTerminal({name}: {name: string}) {
  const host = useRef<HTMLDivElement>(null)
  const [error, setError] = useState('')
  const [maximized, setMaximized] = useState(false)

  useEffect(() => {
    const el = host.current
    if (!el) return
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
    const offExit = EventsOn(`sandbox-terminal:${id}:exit`, (msg: string) =>
      term.write(`\r\n\x1b[90m[session ended${msg ? `: ${msg}` : ''}]\x1b[0m\r\n`))
    const onData = term.onData(data => SandboxTerminalInput(id, data).catch(() => {}))
    const ro = new ResizeObserver(() => {
      fit.fit()
      ResizeSandboxTerminal(id, term.cols, term.rows).catch(() => {})
    })
    ro.observe(el)

    OpenSandboxTerminal(name, id, term.cols, term.rows)
      .then(() => term.focus())
      .catch(e => setError(errText(e)))

    return () => {
      ro.disconnect()
      onData.dispose()
      offData()
      offExit()
      CloseSandboxTerminal(id)
      term.dispose()
    }
  }, [name])

  // Maximizing only changes the layout; the resize observer refits the
  // terminal and sends the new size to the shell.
  return (
    <div className={`term-wrap${maximized ? ' term-max' : ''}`}>
      <div className="term-bar">
        <span className="spacer"/>
        <button className="btn btn-sm" onClick={() => setMaximized(m => !m)}
                aria-label={maximized ? 'Restore terminal' : 'Maximize terminal'}>
          <Icon name={maximized ? 'minimize' : 'maximize'} size={13}/>
          {maximized ? 'Restore' : 'Maximize'}
        </button>
      </div>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      <div className="term" ref={host}/>
    </div>
  )
}
