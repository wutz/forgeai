import {useEffect, useState} from 'react'
import {DeleteImage, DeleteImageRepo, ListImages, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate} from './util'

// Images shows the project registry: the push command with the robot
// credentials and the pushed tags with delete buttons.
export function Images() {
  const [repos, setRepos] = useState<app.ImageRepo[]>([])
  const [reg, setReg] = useState<app.Registry | null>(null)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState('')

  const refresh = () => {
    Registry().then(setReg).catch(e => setError(errText(e)))
    ListImages().then(rs => { setRepos(rs); setError('') }).catch(e => setError(errText(e)))
  }
  useEffect(refresh, [])

  const copy = (text: string, key: string) =>
    navigator.clipboard.writeText(text).then(() => setCopied(key), () => {})

  const login = reg?.username && reg?.password
    ? `docker login ${reg.url.split('/')[0]} -u ${reg.username} -p ${reg.password}`
    : ''
  const push = (tag: string) => reg ? `docker tag IMAGE ${reg.url}/${tag} && docker push ${reg.url}/${tag}` : ''

  return (
    <>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Images</h1>
        <span className="muted">{reg?.url || 'registry not configured'}</span>
        <span className="spacer"/>
      </div>

      {reg?.enabled && login && (
        <div className="card" style={{marginBottom: 16}}>
          <div className="card-head"><h3>Push an image</h3>
            <span className="spacer"/>
            <button className="btn btn-sm" onClick={() => copy(login, 'login')}>
              {copied === 'login' ? 'Copied!' : 'Copy login'}
            </button>
          </div>
          <pre className="code" style={{margin: 0, padding: 16}}>{login}</pre>
        </div>
      )}

      <div className="card">
        {repos.length === 0 && reg?.enabled &&
          <div className="empty">No images pushed yet.</div>}
        {repos.length === 0 && !reg?.enabled &&
          <div className="empty">Registry is not configured for this project.</div>}
        {repos.map(r => (
          <details key={r.name} className="repo">
            <summary>
              <span className="mono">{reg?.url ? reg.url.split('/')[0] : ''}/{r.name}</span>
              <span className="muted"> {r.count} tags · {r.pulls} pulls</span>
              <button className="btn btn-sm btn-danger" onClick={e => {
                e.preventDefault()
                confirm(`Delete repository ${r.name} with all its tags?`) &&
                  DeleteImageRepo(r.name).then(refresh).catch(e => setError(errText(e)))
              }}>Delete all</button>
            </summary>
            <table className="table">
              <thead><tr><th>Tag</th><th>Digest</th><th>Size</th><th>Pushed</th><th/></tr></thead>
              <tbody>
              {r.images?.map(img => (img.tags?.length ? img.tags : ['']).map(tag => (
                <tr key={img.digest + tag}>
                  <td className="mono">{(tag || '').split(',')[0] || <span className="muted">untagged</span>}</td>
                  <td className="mono trunc" title={img.digest}>{img.digest.slice(7, 19)}</td>
                  <td>{img.sizeMiB ? `${img.sizeMiB} MiB` : '—'}</td>
                  <td className="muted">{fmtDate(img.pushedAt)}</td>
                  <td className="actions">
                    <button className="btn btn-sm" onClick={() => copy(push(`${r.name}:${tag}`), 'push')}>
                      {copied === 'push' ? 'Copied!' : 'Tag & push'}
                    </button>
                    <button className="btn btn-sm btn-danger" onClick={() =>
                      confirm(`Delete ${r.name}:${tag}?`) &&
                      DeleteImage(r.name, img.digest).then(refresh).catch(e => setError(errText(e)))}>
                      Delete
                    </button>
                  </td>
                </tr>
              )))}
              </tbody>
            </table>
          </details>
        ))}
      </div>
    </>
  )
}
