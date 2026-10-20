import {useEffect, useState} from 'react'
import {app} from '../wailsjs/go/models'

// Scope is the cluster and project picker at the top of the sidebar. Picking
// either one connects straight away; App remembers the choice.
export function Scope({contexts, cluster, projects, namespace, connected, busy, onCluster, onProject}: {
  contexts: app.KubeContext[],
  cluster: string,
  projects: app.ProjectNamespace[],
  namespace: string,
  connected: boolean,
  busy: boolean,
  onCluster: (name: string) => void,
  onProject: (namespace: string) => void,
}) {
  return (
    <div className="scope">
      <div className="scope-field">
        <label className="nav-label" htmlFor="scope-cluster">Cluster</label>
        <select id="scope-cluster" className="select" value={cluster} disabled={busy || !contexts.length}
                onChange={e => onCluster(e.target.value)}>
          {contexts.map(c => <option key={c.name} value={c.name}>{c.name}</option>)}
        </select>
      </div>
      <div className="scope-field">
        <label className="nav-label" htmlFor="scope-project">Project</label>
        {projects.length ? (
          <select id="scope-project" className="select mono" value={namespace} disabled={busy || !connected}
                  onChange={e => onProject(e.target.value)}>
            {projects.map(p => <option key={p.namespace} value={p.namespace}>{p.namespace} ({p.project})</option>)}
          </select>
        ) : (
          <NamespaceInput value={namespace} disabled={busy || !connected} onCommit={onProject}/>
        )}
      </div>
    </div>
  )
}

// NamespaceInput is the project field for users that may not list projects:
// they type the namespace, committed on Enter or when leaving the field.
function NamespaceInput({value, disabled, onCommit}: {
  value: string,
  disabled: boolean,
  onCommit: (namespace: string) => void,
}) {
  const [draft, setDraft] = useState(value)
  useEffect(() => setDraft(value), [value])

  const commit = () => {
    const namespace = draft.trim()
    if (namespace && namespace !== value) onCommit(namespace)
    else setDraft(value)
  }

  return (
    <input id="scope-project" className="input mono" value={draft} disabled={disabled}
           placeholder="Context default, e.g. forge-myteam" spellCheck={false}
           onChange={e => setDraft(e.target.value)} onBlur={commit}
           onKeyDown={e => { if (e.key === 'Enter') e.currentTarget.blur() }}/>
  )
}
