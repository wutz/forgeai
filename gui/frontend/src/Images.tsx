import {useEffect, useState} from 'react'
import {DeleteImage, DeleteImageRepo, ListImages, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate} from './util'
import {Badge, Card, Empty, Icon, PageHeader, useConfirm} from './ui'

// Images shows the project registry: the push command with the robot
// credentials and the pushed tags with delete buttons.
export function Images() {
  const [repos, setRepos] = useState<app.ImageRepo[]>([])
  // reg stays null until Registry answers, so the empty states do not flash.
  const [reg, setReg] = useState<app.Registry | null>(null)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const confirm = useConfirm()

  const refresh = () => {
    Registry().then(setReg).catch(e => setError(errText(e)))
    ListImages().then(rs => { setRepos(rs); setError('') }).catch(e => setError(errText(e)))
  }
  useEffect(refresh, [])

  const copy = (text: string, key: string) =>
    navigator.clipboard.writeText(text).then(() => {
      setCopied(key)
      setTimeout(() => setCopied(''), 1500)
    }, () => {})

  // The password is not part of the command: docker asks for it, so it stays
  // out of shell history and the process list.
  const host = reg?.url.split('/')[0] ?? ''
  const login = reg?.username ? `docker login ${host} -u ${reg.username}` : ''
  const push = (tag: string) => reg ? `docker tag <local-image> ${reg.url}/${tag} && docker push ${reg.url}/${tag}` : ''
  const totalTags = repos.reduce((n, r) => n + r.count, 0)
  const totalPulls = repos.reduce((n, r) => n + r.pulls, 0)

  const deleteRepo = (r: app.ImageRepo) => confirm.ask({
    title: `Delete repository ${r.name}?`,
    message: `All ${r.count} tags and their images are removed. This cannot be undone.`,
    action: 'Delete repository', danger: true,
    run: () => DeleteImageRepo(r.name).then(refresh).catch(e => setError(errText(e))),
  })
  // Harbor deletes an image by digest, so every tag on it goes with the row.
  const deleteImage = (r: app.ImageRepo, img: app.Image) => {
    const tags = img.tags?.filter(Boolean) ?? []
    confirm.ask({
      title: `Delete ${img.digest.slice(7, 19)} from ${r.name}?`,
      message: tags.length > 1
        ? `This image also carries ${tags.join(', ')}; deleting it removes all of those tags.`
        : 'Its tags, if any, are removed with it. This cannot be undone.',
      action: 'Delete image', danger: true,
      run: () => DeleteImage(r.name, img.digest).then(refresh).catch(e => setError(errText(e))),
    })
  }

  return (
    <div className="page">
      <PageHeader eyebrow="Storage" title="Images"
                  meta={<span className="muted mono" style={{fontSize: 12}}>
                    {reg?.url || 'registry not configured'}
                  </span>}
                  actions={<button className="btn btn-sm btn-ghost" onClick={refresh}>
                    <Icon name="refresh" size={14}/>Refresh
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      {reg?.enabled && login && (
        <Card title="Push an image" subtitle="Log in with the project robot account"
              className="card-spaced"
              actions={<button className="btn btn-sm" onClick={() => copy(login, 'login')}>
                <Icon name={copied === 'login' ? 'check' : 'copy'} size={13}/>
                {copied === 'login' ? 'Copied' : 'Copy login'}
              </button>}>
          <pre className="code" style={{margin: 0, padding: 14, borderRadius: 0, border: 0}}>{login}</pre>
          <div className="robot-password">
            <span className="muted">Password</span>
            <code className="mono">{showPassword ? reg.password : '••••••••••••'}</code>
            <button className="btn btn-sm" onClick={() => setShowPassword(v => !v)}>
              {showPassword ? 'Hide' : 'Show'}
            </button>
            <button className="btn btn-sm" onClick={() => copy(reg.password ?? '', 'password')}>
              <Icon name={copied === 'password' ? 'check' : 'copy'} size={13}/>
              {copied === 'password' ? 'Copied' : 'Copy password'}
            </button>
          </div>
        </Card>
      )}

      <Card title="Repositories"
            subtitle={repos.length ? `${repos.length} repositories · ${totalTags} tags · ${totalPulls} pulls` : undefined}>
        {repos.length === 0 && reg?.enabled && (
          <Empty icon="image" title="No images pushed yet">
            Tag a local image and push it with the command above.
          </Empty>
        )}
        {repos.length === 0 && reg && !reg.enabled && (
          <Empty icon="image" title="Registry is not configured">
            This project has no registry; ask your admin to enable one.
          </Empty>
        )}
        {repos.map(r => (
          <details key={r.name} className="repo">
            <summary>
              <span className="mono">{host}/{r.name}</span>
              <Badge>{r.count} tags</Badge>
              <span className="muted" style={{fontSize: 12}}>{r.pulls} pulls</span>
              <button className="btn btn-sm btn-danger" onClick={e => {
                e.preventDefault()
                deleteRepo(r)
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
                  <td>
                    <div className="actions">
                      <button className="btn btn-sm" disabled={!tag}
                              title={tag ? undefined : 'Untagged images cannot be pushed by tag'}
                              onClick={() => copy(push(`${r.name}:${tag}`), `push-${img.digest}-${tag}`)}>
                        <Icon name={copied === `push-${img.digest}-${tag}` ? 'check' : 'copy'} size={13}/>
                        {copied === `push-${img.digest}-${tag}` ? 'Copied' : 'Tag & push'}
                      </button>
                      <button className="btn btn-sm btn-danger" onClick={() => deleteImage(r, img)}>
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              )))}
              </tbody>
            </table>
          </details>
        ))}
      </Card>
    </div>
  )
}
