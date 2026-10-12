import {FormEvent, useEffect, useState} from 'react'
import {main} from '../wailsjs/go/models'

type Props = {
  contexts: main.KubeContext[]
  saved: {ctx?: string, ns?: string, user?: string}
  loading: boolean
  onConnect: (ctx: string, ns: string, user: string) => void
}

export function ConnectForm({contexts, saved, loading, onConnect}: Props) {
  const [ctx, setCtx] = useState(saved.ctx || '')
  const [ns, setNs] = useState(saved.ns || '')
  const [user, setUser] = useState(saved.user || '')

  useEffect(() => {
    if (!ctx && contexts.length) setCtx((contexts.find(c => c.current) || contexts[0]).name)
  }, [contexts])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    onConnect(ctx, ns.trim(), user.trim())
  }

  return (
    <div className="connect card">
      <form className="card-body" onSubmit={submit}>
        <div>
          <h2>Connect to a cluster</h2>
          <p>forgeai talks to the Kubernetes API directly using your kubeconfig.</p>
        </div>
        <div className="field">
          <label htmlFor="ctx">Kubeconfig context</label>
          <select id="ctx" className="select" value={ctx} onChange={e => setCtx(e.target.value)} required>
            {contexts.map(c => <option key={c.name} value={c.name}>{c.name}{c.current ? ' (current)' : ''}</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ns">Job namespace</label>
          <input id="ns" className="input" value={ns} onChange={e => setNs(e.target.value)} placeholder="Context default, e.g. pod-prod"/>
        </div>
        <div className="field">
          <label htmlFor="user">User ID</label>
          <input id="user" className="input" value={user} onChange={e => setUser(e.target.value)} placeholder="Leave empty to see all users"/>
          <span className="hint">New jobs are created for this user.</span>
        </div>
        <button className="btn btn-primary" type="submit" disabled={loading || !ctx}>
          {loading ? 'Connecting…' : 'Connect'}
        </button>
      </form>
    </div>
  )
}
