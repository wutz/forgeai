import {FormEvent, useEffect, useState} from 'react'
import {
  Config, DeleteProject, GetProject, ListProjects, MemberKubeconfig, SaveProject,
} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate, generateName, isConflict, phaseLabel} from './util'
import {Badge, Card, CancelButton, Dialog, Empty, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'
import {t} from './i18n'

// Admin manages Projects: quota (GPU classes, storage), network isolation,
// members and their kubeconfigs. Shown only to platform admins.
export function Admin() {
  // projects is null until the first read, so a failed first read is not shown as an empty list.
  const [projects, setProjects] = useState<app.Project[] | null>(null)
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

  const list = projects ?? []
  const ready = list.filter(p => p.ready).length
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
                  meta={projects !== null && <span className="muted" style={{fontSize: 12.5}}>
                    {list.length === 1 ? t('admin.metaOne', {ready}) : t('admin.meta', {total: list.length, ready})}
                  </span>}
                  actions={<>
                    <RefreshButton onRefresh={refresh}/>
                    <button className="btn" onClick={() => edit('')}>
                      <Icon name="plus" size={14}/>{t('admin.newProject')}
                    </button>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}
      {editing && <ProjectDialog form={editing} onClose={() => setEditing(null)}
                                 onSaved={() => { setEditing(null); refresh().catch(() => undefined) }}/>}

      <Card>
        {!projects ? (
          error ? null : <Spinner label={t('common.loading')}/>
        ) : projects.length === 0 ? (
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
// typed into the quota, so a half-typed class never reaches the Project. A class
// listed twice is rejected too: the second count would silently replace the first.
function parseGPUs(text: string): Record<string, number> {
  const gpus: Record<string, number> = {}
  for (const pair of text.split(/\s+/).filter(Boolean)) {
    const m = /^([^:]+):(\d+)$/.exec(pair)
    if (!m) throw new Error(t('admin.gpuFormatError', {pair}))
    if (m[1] in gpus) throw new Error(t('admin.gpuDuplicate', {cls: m[1]}))
    if (+m[2] > 0) gpus[m[1]] = +m[2]
  }
  return gpus
}

// The forms the backend reads: resource.ParseQuantity for storage and
// time.ParseDuration for trash retention. Checking them here means a bad value
// is named under its field, not returned by the server after Save.
const quantityRE = /^[+-]?(\d+(\.\d*)?|\.\d+)(Ki|Mi|Gi|Ti|Pi|Ei|[numkMGTPE]|[eE][+-]?\d+)?$/
const durationRE = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$|^0$/
// Project names and member usernames are DNS labels of at most 40 characters
// (the CRD rule), so a bad one is named here rather than rejected after Save.
const dnsLabelRE = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/
const dnsLabelOK = (s: string) => dnsLabelRE.test(s) && s.length <= 40

function ProjectDialog({form, onClose, onSaved}: {
  form: app.Project, onClose: () => void, onSaved: () => void
}) {
  const [p, setP] = useState(() => app.Project.createFrom({...form, name: form.name || generateName('project')}))
  // gpuText is the draft of the GPU field; it is parsed on save.
  const [gpuText, setGpuText] = useState(() => gpuLines(form.quota?.gpus))
  const [error, setError] = useState('')
  // stale is set when the save was refused because the project changed since it was loaded.
  const [stale, setStale] = useState(false)
  const [busy, setBusy] = useState(false)
  const [kube, setKube] = useState('')
  const [kubeError, setKubeError] = useState('')
  // prefix is the platform's namespace prefix, so the preview names the namespace
  // the controller will create. A failed read only hides the preview.
  const [prefix, setPrefix] = useState('')
  useEffect(() => {
    let live = true
    Config().then(c => { if (live) setPrefix(c.namespacePrefix) }).catch(() => undefined)
    return () => { live = false }
  }, [])
  const create = !form.name
  // gpuErr is the message for the first bad GPU pair, or '' when the field parses.
  const gpuErr = (() => {
    try { parseGPUs(gpuText); return '' } catch (err) { return errText(err) }
  })()
  const nameOK = !p.name || dnsLabelOK(p.name)
  const nsPreview = prefix && p.name && nameOK ? prefix + p.name : ''
  const storageOK = !p.quota?.storage || quantityRE.test(p.quota.storage)
  const registryOK = !p.quota?.registryStorage || quantityRE.test(p.quota.registryStorage)
  const trashOK = !p.trashRetention || durationRE.test(p.trashRetention)
  // Counts are whole numbers: a fraction such as 1.5 is refused rather than saved.
  const volumesOK = Number.isInteger(p.quota?.volumes ?? 0) && (p.quota?.volumes ?? 0) >= 0
  const podsOK = Number.isInteger(p.quota?.pods ?? 0) && (p.quota?.pods ?? 0) >= 0
  // Empty rows are reported on Save; here a name that is not a DNS label, or
  // a username listed twice, is named under the Members field.
  const memberErr = (() => {
    const seen = new Set<string>()
    for (const m of p.members || []) {
      const name = m.name.trim()
      if (!name) continue
      if (!dnsLabelOK(name)) return t('admin.memberNameInvalid')
      if (seen.has(name)) return t('admin.memberDuplicate', {name})
      seen.add(name)
    }
    return ''
  })()
  const formOK = !gpuErr && nameOK && storageOK && registryOK && trashOK && volumesOK && podsOK && !memberErr

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
      // Names are saved as typed-trimmed, the same value the validation above checks.
      project = app.Project.createFrom({
        ...p,
        quota: app.ProjectQuota.createFrom({...p.quota, gpus: parseGPUs(gpuText)}),
        members: (p.members || []).map(m => app.ProjectMember.createFrom({name: m.name.trim(), role: m.role})),
      })
    } catch (err) {
      setError(errText(err))
      return
    }
    setError('')
    setBusy(true)
    SaveProject(project).then(onSaved).catch(err => { setError(errText(err)); setStale(isConflict(err)); setBusy(false) })
  }
  // A stale save is refused, so the project is read again. The reload replaces the
  // unsaved edits with the saved project; the form is the only place they live.
  const reload = () => GetProject(p.name).then(fresh => {
    setP(fresh)
    setGpuText(gpuLines(fresh.quota?.gpus))
    setError('')
    setStale(false)
  }).catch(e => setError(errText(e)))
  // The export error stays by the member list, where the button was clicked,
  // and the previous member's YAML is dropped so it cannot be mistaken for this one.
  const exportKube = (member: string) => {
    setKubeError('')
    MemberKubeconfig(p.name, member)
      .then(y => setKube(`${member}:\n${y}`))
      .catch(e => { setKube(''); setKubeError(errText(e)) })
  }
  const downloadKube = () => {
    const blob = new Blob([kube.split('\n').slice(1).join('\n')], {type: 'text/yaml'})
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `${p.name}-${kube.split(':')[0]}.yaml`
    a.click()
    setTimeout(() => URL.revokeObjectURL(a.href), 0)
  }

  return (
    <Dialog title={create ? t('admin.newProject') : t('admin.editProject')}
            subtitle={t('admin.dialogSubtitle')}
            onClose={onClose} busy={busy} footer={<>
              <CancelButton disabled={busy}/>
              <button type="submit" form="project-form" className={create ? 'btn' : 'btn btn-primary'} disabled={busy || !p.name || !formOK}>
                {busy ? t('admin.saving') : create ? t('admin.createProject') : t('common.save')}
              </button>
            </>}>
      <form id="project-form" className="dialog-form" onSubmit={save}>
        {error && <div className="banner banner-error" role="alert">
          <span>{error}</span>
          {stale && <>
            <span className="spacer"/>
            <button type="button" className="btn btn-sm" title={t('admin.reloadProjectHint')} onClick={reload}>
              {t('admin.reloadProject')}
            </button>
          </>}
        </div>}
        <div className="row">
          <div className="field">
            <label htmlFor="p-name">{t('common.name')}</label>
            <input id="p-name" className="input mono" disabled={!create} value={p.name}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('name', e.target.value)} aria-invalid={!nameOK} required/>
            <span className="hint">{t('admin.nameHint')}</span>
            {!nameOK && <span className="hint hint-error">{t('admin.nameInvalid')}</span>}
            {nsPreview && <span className="hint">{t('admin.namespacePreview', {namespace: nsPreview})}</span>}
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
                 onChange={e => setGpuText(e.target.value)} aria-invalid={!!gpuErr}
                 placeholder="rtx-5090:8 a100:2"/>
          <span className="hint">{t('admin.jobsOverQuota')}</span>
          {gpuErr && <span className="hint hint-error">{gpuErr}</span>}
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-storage">{t('admin.storage')}</label>
            <input id="p-storage" className="input mono" value={p.quota?.storage || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, storage: e.target.value}))}
                   aria-invalid={!storageOK} placeholder="1Ti"/>
            <span className="hint">{t('admin.emptyNoLimit')}</span>
            {!storageOK && <span className="hint hint-error">{t('admin.quantityInvalid')}</span>}
          </div>
          <div className="field">
            <label htmlFor="p-registry-storage">{t('admin.registryStorage')}</label>
            <input id="p-registry-storage" className="input mono" value={p.quota?.registryStorage || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, registryStorage: e.target.value}))}
                   aria-invalid={!registryOK} placeholder="100Gi"/>
            <span className="hint">{t('admin.emptyNoLimit')}</span>
            {!registryOK && <span className="hint hint-error">{t('admin.quantityInvalid')}</span>}
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-volumes">{t('admin.volumes')}</label>
            <input id="p-volumes" className="input" type="number" min={0} step={1}
                   value={p.quota?.volumes || 0}
                   aria-invalid={!volumesOK}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, volumes: +e.target.value}))}/>
            <span className="hint">{t('admin.zeroNoLimit')}</span>
            {!volumesOK && <span className="hint hint-error">{t('admin.countInvalid')}</span>}
          </div>
          <div className="field">
            <label htmlFor="p-pods">{t('admin.pods')}</label>
            <input id="p-pods" className="input" type="number" min={0} step={1}
                   value={p.quota?.pods || 0}
                   aria-invalid={!podsOK}
                   onChange={e => set('quota', app.ProjectQuota.createFrom({...p.quota, pods: +e.target.value}))}/>
            <span className="hint">{t('admin.zeroNoLimit')}</span>
            {!podsOK && <span className="hint hint-error">{t('admin.countInvalid')}</span>}
          </div>
        </div>
        <div className="row">
          <div className="field">
            <label htmlFor="p-trash">{t('admin.trashRetention')}</label>
            <input id="p-trash" className="input mono" value={p.trashRetention || ''}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   onChange={e => set('trashRetention', e.target.value)} aria-invalid={!trashOK}
                   placeholder="168h"/>
            <span className="hint">{t('admin.trashHint')}</span>
            {!trashOK && <span className="hint hint-error">{t('admin.durationInvalid')}</span>}
          </div>
          <div className="field">
            <label htmlFor="p-deletion">{t('admin.deletionPolicy')}</label>
            <select id="p-deletion" className="select" value={p.deletionPolicy || 'Retain'}
                    onChange={e => set('deletionPolicy', e.target.value)}>
              <option value="Retain">{t('admin.policyRetain')}</option>
              <option value="Delete">{t('admin.policyDelete')}</option>
            </select>
            <span className="hint">{t(p.deletionPolicy === 'Delete' ? 'admin.policyDeleteHint' : 'admin.policyRetainHint')}</span>
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
            <div className="row member-row" key={i}>
              <input className="input mono" value={m.name} placeholder={t('admin.usernamePlaceholder')}
                     aria-label={t('admin.memberName')}
                     autoCapitalize="none" autoCorrect="off" spellCheck={false}
                     onChange={e => setMember(i, e.target.value, m.role)}/>
              <select className="select" value={m.role} aria-label={t('admin.memberRole')}
                      onChange={e => setMember(i, m.name, e.target.value)}>
                <option value="edit">{t('admin.roleEdit')}</option>
                <option value="view">{t('admin.roleView')}</option>
              </select>
              {!create && <button type="button" className="btn btn-sm"
                                  onClick={() => exportKube(m.name.trim())} disabled={!m.name.trim()}>{t('admin.kubeconfig')}</button>}
              <button type="button" className="btn btn-sm btn-danger" aria-label={t('admin.removeMember')}
                      title={t('admin.removeMember')}
                      onClick={() => set('members', (p.members || []).filter((_, j) => j !== i))}>
                <Icon name="trash" size={13}/>
              </button>
            </div>
          ))}
          {memberErr && <span className="hint hint-error">{memberErr}</span>}
          {kubeError && <div className="banner banner-error" role="alert">{kubeError}</div>}
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
