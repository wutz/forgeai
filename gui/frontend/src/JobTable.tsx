import {main} from '../wailsjs/go/models'
import {Status} from './util'

type Props = {
  jobs: main.Job[]
  selected: string | null
  onSelect: (name: string) => void
}

export function JobTable({jobs, selected, onSelect}: Props) {
  if (!jobs.length) {
    return <div className="card empty">No jobs yet. Create one with “New job”.</div>
  }
  return (
    <div className="card">
      <table className="table">
        <thead>
          <tr>
            <th>Job</th><th>Status</th><th>Resource</th><th>Workers</th><th>Image</th><th>Age</th>
          </tr>
        </thead>
        <tbody>
          {jobs.map(j => (
            <tr key={j.name} className={j.name === selected ? 'selected' : ''}
                onClick={() => onSelect(j.name)} tabIndex={0}
                onKeyDown={e => { if (e.key === 'Enter') onSelect(j.name) }}
                aria-selected={j.name === selected}>
              <td>
                <div className="mono">{j.jobID}</div>
                <div className="muted trunc" style={{fontSize: 12}}>{j.userID}</div>
              </td>
              <td><Status phase={j.phase}/></td>
              <td className="mono">{j.resourceID}</td>
              <td>{j.ready}/{j.workers}</td>
              <td className="mono trunc" title={j.image}>{j.image}</td>
              <td className="muted">{j.age}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
