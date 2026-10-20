import {useEffect, useState} from 'react'
import {
  DeleteProject, GetProject, ListProjects, MemberKubeconfig, SaveProject,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate} from './util'
import {Badge, Card, Dialog, Empty, Icon, PageHeader} from './ui'

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

  const ready = projects.filter(p => p.ready).length

  return (
    <div className="page">
      <PageHeader eyebrow="Admin" title="Projects"
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {projects.length} projects · {ready} ready
                  </span>}
                  actions={<button className="btn btn-primary" onClick={() => edit('')}>
                    <Icon name="plus" size={14}/>New project
                  </button>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {editing && <ProjectDialog form={editing} onClose={() => setEditing(null)}
                                 onSaved={() => { setEditing(null); refresh() }}/>}

      <Card>
        {projects.length === 0 ? (
          <Empty icon="shield" title="No projects yet">
            A project is a namespace with a GPU quota, members and an isolated network.
          </Empty>
        ) : (
          <table className="table">
            <thead>
            <tr><th>Project</th><th>Status</th><th>GPUs</th><th>Storage</th>
              <th>Network</th><th>Members</th><th>Created</th><th/></tr>
            </thead>
            <tbody>
            {projects.map(p => (
              <tr key={p.name}>
                <td className="mono">
                  <span className="cell-main">{p.name}</span>
                  <span className="cell-sub">{p.namespace || 'no namespace'}</span>
                </td>
                <td><span className={`status status-${p.ready ? 'running' : 'pending'}`}>
                  <span className="dot" aria-hidden/>{p.ready ? 'Ready' : 'Pending'}</span></td>
                <td>
                  <div className="actions">
                    {Object.entries(p.quota?.gpus || {}).map(([c, n]) =>
                      <Badge key={c}>{c} · {n}</Badge>)}
                    {!Object.keys(p.quota?.gpus || {}).length && <span className="muted">—</span>}
                  </div>
                </td>
                <td className="mono">{p.quota?.storage || '—'}</td>
                <td>
                  {p.network?.isolation === 'None'
                    ? <span className="muted">None</span>
                    : <Badge tone="accent">Isolated</Badge>}
                  {p.network?.denyInternet && <span className="muted" style={{fontSize: 12}}> · no internet</span>}
                </td>
                <td className="muted">{p.members?.map(m => m.name).join(', ') || '—'}</td>
                <td className="muted">{fmtDate(p.created)}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" onClick={() => edit(p.name)}>Edit</button>
                    <button className="btn btn-sm btn-danger" onClick={() =>
                      confirm(`Delete project ${p.name}? With deletionPolicy Delete this removes the
namespace, its volumes and the Harbor project.`) &&
                      DeleteProject(p.name).then(refresh).catch(e => setError(errText(e)))}>Delete</button>
                  </div>
                </td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </Card>
    </div>
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
    <Dialog title={`${create ? 'New' : 'Edit'} project`}
            subtitle="Namespace, GPU quota, network isolation and members"
            onClose={onClose} footer={<>
              <button className="btn" onClick={onClose}>Cancel</button>
              <button className="btn btn-primary" onClick={save} disabled={busy || !p.name}>
                {busy ? 'Saving…' : create ? 'Create project' : 'Save'}
              </button>
            </>}>
      <form className="dialog-form" onSubmit={e => { e.preventDefault(); save() }}>
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
                                  onClick={() => exportKube(m.name)} disabled={!m.name}>Kubeconfig</button>}
              <button type="button" className="btn btn-sm btn-danger"
                      onClick={() => set('members', p.members.filter((_, j) => j !== i))}>
                <Icon name="trash" size={13}/>
              </button>
            </div>
          ))}
          <button type="button" className="btn btn-sm" style={{alignSelf: 'flex-start'}}
                  onClick={() => set('members', [...(p.members || []),
                    app.ProjectMember.createFrom({name: '', role: 'edit'})])}>
            <Icon name="plus" size={13}/>Add member
          </button>
        </div>

        {kube && (
          <div className="field">
            <div className="log-bar">
              <span className="mono">{kube.split(':')[0]}.yaml</span>
              <span className="spacer"/>
              <button type="button" className="btn btn-sm" onClick={downloadKube}>
                <Icon name="download" size={13}/>Download
              </button>
            </div>
            <pre className="code" style={{maxHeight: 200}}>{kube.split('\n').slice(1).join('\n')}</pre>
          </div>
        )}
      </form>
    </Dialog>
  )
}
