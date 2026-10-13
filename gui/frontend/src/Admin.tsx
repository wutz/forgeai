import {useEffect, useState} from 'react'
import {
  DeleteProject, GetProject, ListProjects, MemberKubeconfig, SaveProject,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate} from './util'

// Admin manages Projects: quota (GPU classes, storage), network isolation,
// members and their kubeconfigs. Shown only to platform admins.
export function Admin() {
  const [projects, setProjects] = useState<app.Project[]>([])
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<app.Project | null>(null)

  const refresh = () => ListProjects().then(ps => {
    setProjects(ps); setError('')
  }).catch(e => setError(errText(e)))
  useEffect(() => { refresh() }, [])

  const newProject = () => app.Project.createFrom({
    name: '', displayName: '', namespace: '',
    quota: app.ProjectQuota.createFrom({gpus: {}, volumes: 0, pods: 0}),
    network: app.ProjectNetwork.createFrom({isolation: 'Isolated', denyInternet: false}),
    members: [], deletionPolicy: 'Retain', ready: false, registry: '', created: '',
    trashRetention: '',
  })

  const edit = (name: string) =>
    (name ? GetProject(name) : Promise.resolve(newProject())).then(setEditing).catch(e => setError(errText(e)))

  return (
    <>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Projects</h1>
        <span className="muted">{projects.length}</span>
        <span className="spacer"/>
        <button className="btn btn-primary" onClick={() => edit('')}>
          New project
        </button>
      </div>
      {editing && <ProjectDialog form={editing} onClose={() => setEditing(null)}
                                 onSaved={() => { setEditing(null); refresh() }}/>}

      <div className="card">
        <table className="table">
          <thead>
          <tr><th>Project</th><th>Namespace</th><th>Ready</th><th>GPUs</th><th>Isolation</th>
            <th>Members</th><th>Created</th></tr>
          </thead>
          <tbody>
          {projects.map(p => (
            <tr key={p.name}>
              <td className="mono">{p.name}
                {p.displayName && <div className="muted">{p.displayName}</div>}
              </td>
              <td className="mono">{p.namespace || '—'}</td>
              <td><span className={`status status-${p.ready ? 'running' : 'pending'}`}>
                <span className="dot" aria-hidden/>{p.ready ? 'Ready' : 'Pending'}</span>
              </td>
              <td className="mono">{Object.entries(p.quota?.gpus || {})
                .map(([c, n]) => `${c}:${n}`).join(' ') || '—'}</td>
              <td>{p.network?.isolation}{p.network?.denyInternet ? ' · no internet' : ''}</td>
              <td>{p.members?.map(m => m.name).join(', ') || '—'}</td>
              <td className="muted">{fmtDate(p.created)}</td>
              <td className="actions">
                <button className="btn btn-sm" onClick={() =>
                  GetProject(p.name).then(setEditing).catch(e => setError(errText(e)))}>Edit</button>
                <button className="btn btn-sm btn-danger" onClick={() =>
                  confirm(`Delete project ${p.name}? With deletionPolicy Delete this removes the
namespace, its volumes and the Harbor project.`) &&
                  DeleteProject(p.name).then(refresh).catch(e => setError(errText(e)))}>Delete</button>
              </td>
            </tr>
          ))}
          </tbody>
        </table>
      </div>
    </>
  )
}

function ProjectDialog({form, onClose, onSaved}: {
  form: app.Project, onClose: () => void, onSaved: () => void
}) {
  const [p, setP] = useState(app.Project.createFrom({...form}))
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [kube, setKube] = useState('')
  const create = !form.name

  const set = <K extends keyof app.Project>(k: K, v: app.Project[K]) =>
    setP(prev => app.Project.createFrom({...prev, [k]: v}))

  const setGPU = (cls: string, n: string) => {
    const gpus = {...(p.quota?.gpus || {})}
    if (n === '') delete gpus[cls]
    else gpus[cls] = +n
    set('quota', app.ProjectQuota.createFrom({...p.quota, gpus}))
  }
  const setMember = (i: number, name: string, role: string) => {
    const members = [...(p.members || [])]
    members[i] = app.ProjectMember.createFrom({name, role})
    set('members', members)
  }

  const save = () => {
    setBusy(true)
    SaveProject(p).then(onSaved).catch(e => { setError(errText(e)); setBusy(false) })
  }
  const exportKube = (member: string) => {
    MemberKubeconfig(p.name, member).then(y => setKube(`${member}:\n${y}`)).catch(e => setError(errText(e)))
  }
  const downloadKube = () => {
    const blob = new Blob([kube.split('\n').slice(1).join('\n')], {type: 'text/yaml'})
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${p.name}-${kube.split(':')[0]}.yaml`
    a.click()
  }

  return (
    <div className="overlay">
      <div className="dialog">
        <form onSubmit={e => { e.preventDefault(); save() }}>
          <h2>{create ? 'New' : 'Edit'} project</h2>
          {error && <div className="banner banner-error">{error}</div>}
          <div className="row">
            <div className="field">
              <label htmlFor="p-name">Name</label>
              <input id="p-name" className="input mono" disabled={!create} value={p.name}
                     onChange={e => set('name', e.target.value)} required/>
            </div>
            <div className="field">
              <label htmlFor="p-display">Display name</label>
              <input id="p-display" className="input" value={p.displayName || ''}
                     onChange={e => set('displayName', e.target.value)}/>
            </div>
          </div>
          <div className="field">
            <label htmlFor="p-gpus">GPU quota per class (class: count)</label>
            <input id="p-gpus" className="input mono"
                   value={Object.entries(p.quota?.gpus || {}).map(([c, n]) => `${c}:${n}`).join(' ')}
                   onChange={e => {
                     const gpus: Record<string, number> = {}
                     for (const pair of e.target.value.split(/\s+/))
                       if (pair) { const [c, n] = pair.split(':'); if (c && n) gpus[c] = +n }
                     set('quota', app.ProjectQuota.createFrom({...p.quota, gpus}))
                   }}
                   placeholder="rtx-5090:8 a100:2"/>
            <span className="hint">Jobs over quota wait in the Kueue queue.</span>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="p-storage">Storage</label>
              <input id="p-storage" className="input mono" value={p.quota?.storage || ''}
                     onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, storage: e.target.value}))}
                     placeholder="1Ti"/>
            </div>
            <div className="field">
              <label htmlFor="p-volumes">Volumes</label>
              <input id="p-volumes" className="input" type="number" min={0}
                     value={p.quota?.volumes || 0}
                     onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, volumes: +e.target.value}))}/>
            </div>
            <div className="field">
              <label htmlFor="p-pods">Pods</label>
              <input id="p-pods" className="input" type="number" min={0}
                     value={p.quota?.pods || 0}
                     onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, pods: +e.target.value}))}/>
            </div>
          </div>
          <div className="row">
            <div className="field">
              <label htmlFor="p-isolation">Network isolation</label>
              <select id="p-isolation" className="select"
                      value={p.network?.isolation || 'Isolated'}
                      onChange={e => set('network', app.ProjectNetwork.createFrom({
                        ...p.network, isolation: e.target.value}))}>
                <option value="Isolated">Isolated (project only)</option>
                <option value="None">None</option>
              </select>
            </div>
            <div className="field">
              <label>Internet egress</label>
              <label className="checkbox">
                <input type="checkbox" checked={!!p.network?.denyInternet}
                       onChange={e => set('network', app.ProjectNetwork.createFrom({
                         ...p.network, denyInternet: e.target.checked }))}/>
                Deny internet (DNS stays allowed)
              </label>
            </div>
          </div>

          <div className="field">
            <label>Members</label>
            {(p.members || []).map((m, i) => (
              <div className="row" key={i}>
                <input className="input mono" value={m.name} placeholder="username"
                       onChange={e => setMember(i, e.target.value, m.role)}/>
                <select className="select" value={m.role}
                        onChange={e => setMember(i, m.name, e.target.value)}>
                  <option value="edit">edit</option>
                  <option value="view">view</option>
                </select>
                {!create && <button type="button" className="btn btn-sm"
                        onClick={() => exportKube(m.name)} disabled={!m.name}>Export kubeconfig</button>
                }
                <button type="button" className="btn btn-sm btn-danger"
                        onClick={() => set('members', p.members.filter((_, j) => j !== i))}>×</button>
              </div>
            ))}
            <button type="button" className="btn btn-sm"
                    onClick={() => set('members', [...(p.members || []),
                      app.ProjectMember.createFrom({name: '', role: 'edit'})])}>Add member</button>
          </div>

          {kube && (
            <div className="field">
              <div className="card-head">
                <h3 className="mono">{kube.split(':')[0]}.yaml</h3>
                <span className="spacer"/>
                <button type="button" className="btn btn-sm" onClick={downloadKube}>Download</button>
              </div>
              <pre className="code" style={{maxHeight: 200, overflow: 'auto'}}>{kube.split('\n').slice(1).join('\n')}</pre>
            </div>
          )}
        </form>
        <div className="dialog-foot">
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={save} disabled={busy || !p.name}>
            {busy ? 'Saving…' : create ? 'Create project' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
