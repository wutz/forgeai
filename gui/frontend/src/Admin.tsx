import {FormEvent, useEffect, useState} from 'react'
import {
  DeleteProject, GetProject, ListProjects, MemberKubeconfig, SaveProject,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate, generateName, phaseLabel} from './util'
import {Badge, Card, Dialog, Empty, Icon, PageHeader, RefreshButton, useActive, useConfirm} from './ui'
import {t} from './i18n'

// Admin manages Projects: quota (GPU classes, storage), network isolation,
// members and their kubeconfigs. Shown only to platform admins.
export function Admin() {
  const [projects, setProjects] = useState<app.Project[]>([])
  const [error, setError] = useState('')
  const [editing, setEditing] = useState<app.Project | null>(null)
  const confirm = useConfirm()
  const active = useActive()

  // refresh rejects when the list fails; the banner shows why and the refresh
  // button reports it.
  const refresh = () => ListProjects().then(ps => {
    setProjects(ps); setError('')
  }).catch(e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => { if (active) refresh().catch(() => undefined) }, [active])

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
  const deleteProject = (p: app.Project) => confirm.ask({
    title: t('admin.deleteTitle', {name: p.name}),
    message: p.deletionPolicy === 'Delete'
      ? t('admin.deleteDataMessage')
      : t('admin.deleteRetainMessage'),
    action: t('admin.deleteAction'), danger: true,
    run: () => DeleteProject(p.name).then(refresh).catch(e => setError(errText(e))),
  })

  return (
    <div className="page">
      <PageHeader eyebrow={t('admin.eyebrow')} title={t('admin.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>
                    {t('admin.meta', {total: projects.length, ready})}
                  </span>}
                  actions={<>
                    <RefreshButton onRefresh={refresh}/>
                    <button className="btn btn-primary" onClick={() => edit('')}>
                      <Icon name="plus" size={14}/>{t('admin.newProject')}
                    </button>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}
      {editing && <ProjectDialog form={editing} onClose={() => setEditing(null)}
                                 onSaved={() => { setEditing(null); refresh().catch(() => undefined) }}/>}

      <Card>
        {projects.length === 0 ? (
          <Empty icon="shield" title={t('admin.noProjects')}>
            {t('admin.noProjectsHint')}
          </Empty>
        ) : (
          <table className="table">
            <thead>
            <tr><th>{t('admin.project')}</th><th>{t('common.status')}</th><th>{t('admin.gpus')}</th><th>{t('admin.storage')}</th>
              <th>{t('admin.network')}</th><th>{t('admin.members')}</th><th>{t('common.created')}</th><th/></tr>
            </thead>
            <tbody>
            {projects.map(p => (
              <tr key={p.name}>
                <td className="mono">
                  <span className="cell-main">{p.name}</span>
                  <span className="cell-sub">{p.namespace || t('admin.noNamespace')}</span>
                </td>
                <td><span className={`status status-${p.ready ? 'running' : 'pending'}`}>
                  <span className="dot" aria-hidden/>{p.ready ? phaseLabel('Ready') : phaseLabel('Pending')}</span></td>
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
                    ? <span className="muted">{t('admin.isolationNone')}</span>
                    : <Badge tone="accent">{t('admin.isolated')}</Badge>}
                  {p.network?.denyInternet && <span className="muted" style={{fontSize: 12}}> · {t('admin.noInternet')}</span>}
                </td>
                <td className="muted">{p.members?.map(m => m.name).join(', ') || '—'}</td>
                <td className="muted">{fmtDate(p.created)}</td>
                <td>
                  <div className="actions">
                    <button className="btn btn-sm" onClick={() => edit(p.name)}>{t('common.edit')}</button>
                    <button className="btn btn-sm btn-danger" onClick={() => deleteProject(p)}>{t('common.delete')}</button>
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

// gpuLines renders GPU quota as the "class:count" pairs the field edits.
function gpuLines(gpus: Record<string, number> = {}): string {
  return Object.entries(gpus).map(([c, n]) => `${c}:${n}`).join(' ')
}

// parseGPUs reads the GPU field. Partial pairs are rejected here rather than
// typed into the quota, so a half-typed class never reaches the Project.
function parseGPUs(text: string): Record<string, number> {
  const gpus: Record<string, number> = {}
  for (const pair of text.split(/\s+/).filter(Boolean)) {
    const m = /^([^:]+):(\d+)$/.exec(pair)
    if (!m) throw new Error(t('admin.gpuFormatError', {pair}))
    if (+m[2] > 0) gpus[m[1]] = +m[2]
  }
  return gpus
}

function ProjectDialog({form, onClose, onSaved}: {
  form: app.Project, onClose: () => void, onSaved: () => void
}) {
  const [p, setP] = useState(() => app.Project.createFrom({...form, name: form.name || generateName('project')}))
  // gpuText is the draft of the GPU field; it is parsed on save.
  const [gpuText, setGpuText] = useState(() => gpuLines(form.quota?.gpus))
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

  const save = (e?: FormEvent) => {
    e?.preventDefault()
    // A member row left empty would be saved as a nameless member; reject it here.
    if ((p.members || []).some(m => !m.name.trim())) {
      setError(t('admin.memberNameRequired'))
      return
    }
    let project: app.Project
    try {
      project = app.Project.createFrom({
        ...p, quota: app.ProjectQuota.createFrom({...p.quota, gpus: parseGPUs(gpuText)}),
      })
    } catch (err) {
      setError(errText(err))
      return
    }
    setError('')
    setBusy(true)
    SaveProject(project).then(onSaved).catch(err => { setError(errText(err)); setBusy(false) })
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
    <Dialog title={create ? t('admin.newProject') : t('admin.editProject')}
            subtitle={t('admin.dialogSubtitle')}
            onClose={onClose} busy={busy} footer={<>
              <button className="btn" onClick={onClose} disabled={busy}>{t('common.cancel')}</button>
              <button type="submit" form="project-form" className="btn btn-primary" disabled={busy || !p.name}>
                {busy ? t('admin.saving') : create ? t('admin.createProject') : t('common.save')}
              </button>
            </>}>
      <form id="project-form" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error">{error}</div>}
        <div className="row">
          <div className="field">
            <label htmlFor="p-name">{t('common.name')}</label>
            <input id="p-name" className="input mono" disabled={!create} value={p.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} required/>
          </div>
          <div className="field">
            <label htmlFor="p-display">{t('admin.displayName')}</label>
            <input id="p-display" className="input" value={p.displayName || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('displayName', e.target.value)}/>
          </div>
        </div>
        <div className="field">
          <label htmlFor="p-gpus">{t('admin.gpuQuotaLabel')}</label>
          <input id="p-gpus" className="input mono" value={gpuText}
                 autoCapitalize="none" autoCorrect="off" spellCheck={false}
                 onChange={e => setGpuText(e.target.value)}
                 placeholder="rtx-5090:8 a100:2"/>
          <span className="hint">{t('admin.jobsOverQuota')}</span>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-storage">{t('admin.storage')}</label>
            <input id="p-storage" className="input mono" value={p.quota?.storage || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, storage: e.target.value}))}
                   placeholder="1Ti"/>
            <span className="hint">{t('admin.emptyNoLimit')}</span>
          </div>
          <div className="field">
            <label htmlFor="p-registry-storage">{t('admin.registryStorage')}</label>
            <input id="p-registry-storage" className="input mono" value={p.quota?.registryStorage || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, registryStorage: e.target.value}))}
                   placeholder="100Gi"/>
            <span className="hint">{t('admin.emptyNoLimit')}</span>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-volumes">{t('admin.volumes')}</label>
            <input id="p-volumes" className="input" type="number" min={0}
                   value={p.quota?.volumes || 0}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, volumes: +e.target.value}))}/>
            <span className="hint">{t('admin.zeroNoLimit')}</span>
          </div>
          <div className="field">
            <label htmlFor="p-pods">Pods</label>
            <input id="p-pods" className="input" type="number" min={0}
                   value={p.quota?.pods || 0}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, pods: +e.target.value}))}/>
            <span className="hint">{t('admin.zeroNoLimit')}</span>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-trash">{t('admin.trashRetention')}</label>
            <input id="p-trash" className="input mono" value={p.trashRetention || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('trashRetention', e.target.value)} placeholder="168h"/>
            <span className="hint">{t('admin.trashHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="p-deletion">{t('admin.deletionPolicy')}</label>
            <select id="p-deletion" className="select" value={p.deletionPolicy || 'Retain'}
                    onChange={e => set('deletionPolicy', e.target.value)}>
              <option value="Retain">{t('admin.policyRetain')}</option>
              <option value="Delete">{t('admin.policyDelete')}</option>
            </select>
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-isolation">{t('admin.networkIsolation')}</label>
            <select id="p-isolation" className="select"
                    value={p.network?.isolation || 'Isolated'}
                    onChange={e => set('network', app.ProjectNetwork.createFrom({
                      ...p.network, isolation: e.target.value}))}>
              <option value="Isolated">{t('admin.isolationIsolated')}</option>
              <option value="None">{t('admin.isolationNone')}</option>
            </select>
            <span className="hint">{t('admin.isolationHint')}</span>
          </div>
          <div className="field">
            <label>{t('admin.internetEgress')}</label>
            <label className="checkbox">
              <input type="checkbox" checked={!!p.network?.denyInternet}
                     onChange={e => set('network', app.ProjectNetwork.createFrom({
                       ...p.network, denyInternet: e.target.checked }))}/>
              {t('admin.denyInternet')}
            </label>
          </div>
        </div>

        <div className="field">
          <label>{t('admin.members')}</label>
          {(p.members || []).map((m, i) => (
            <div className="row" key={i}>
              <input className="input mono" value={m.name} placeholder={t('admin.usernamePlaceholder')}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     onChange={e => setMember(i, e.target.value, m.role)}/>
              <select className="select" value={m.role}
                      onChange={e => setMember(i, m.name, e.target.value)}>
                <option value="edit">{t('admin.roleEdit')}</option>
                <option value="view">{t('admin.roleView')}</option>
              </select>
              {!create && <button type="button" className="btn btn-sm"
                                  onClick={() => exportKube(m.name)} disabled={!m.name}>Kubeconfig</button>}
              <button type="button" className="btn btn-sm btn-danger" aria-label={t('admin.removeMember')}
                      title={t('admin.removeMember')}
                      onClick={() => set('members', (p.members || []).filter((_, j) => j !== i))}>
                <Icon name="trash" size={13}/>
              </button>
            </div>
          ))}
          <button type="button" className="btn btn-sm" style={{alignSelf: 'flex-start'}}
                  onClick={() => set('members', [...(p.members || []),
                    app.ProjectMember.createFrom({name: '', role: 'edit'})])}>
            <Icon name="plus" size={13}/>{t('admin.addMember')}
          </button>
        </div>

        {kube && (
          <div className="field">
            <div className="log-bar">
              <span className="mono">{kube.split(':')[0]}.yaml</span>
              <span className="spacer"/>
              <button type="button" className="btn btn-sm" onClick={downloadKube}>
                <Icon name="download" size={13}/>{t('admin.download')}
              </button>
            </div>
            <pre className="code" style={{maxHeight: 200}}>{kube.split('\n').slice(1).join('\n')}</pre>
          </div>
        )}
      </form>
    </Dialog>
  )
}
