import {useEffect, useState} from 'react'
import {ListInstanceTypes, Quota} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'

// Resources shows the cluster GPU catalogue (instance types with free
// counts) and the project's quota and its use.
export function Resources() {
  const [types, setTypes] = useState<app.InstanceType[]>([])
  const [quota, setQuota] = useState<app.QuotaUsage | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    ListInstanceTypes().then(ts => { setTypes(ts); setError('') }).catch(e => setError(errText(e)))
    Quota().then(setQuota).catch(e => setError(errText(e)))
    const t = setInterval(() => {
      ListInstanceTypes().then(setTypes).catch(() => {})
      Quota().then(setQuota).catch(() => {})
    }, 10000)
    return () => clearInterval(t)
  }, [])

  return (
    <>
      {error && <div className="banner banner-error">{error}</div>}
      <div className="toolbar">
        <h1 className="title">Cluster resources</h1>
        <span className="spacer"/>
      </div>

      <div className="card">
        <div className="card-head"><h3>Instance types</h3></div>
        {types.length === 0 && <div className="empty">No GPU nodes detected.</div>}
        {types.length > 0 && (
          <table className="table">
            <thead>
            <tr><th>Type</th><th>GPU</th><th>CPU / worker</th><th>Memory / worker</th>
              <th>Capacity</th><th>Free</th></tr>
            </thead>
            <tbody>
            {types.map(t => (
              <tr key={t.name} className={t.free >= 1 ? '' : 'row-full'}>
                <td className="mono">{t.name}</td>
                <td>{t.gpuCount}× {t.product}</td>
                <td>{t.cpu}</td>
                <td>{t.memory}</td>
                <td>{t.total}</td>
                <td><strong className={t.free >= 1 ? 'free' : 'muted'}>{t.free}</strong></td>
              </tr>
            ))}
            </tbody>
          </table>
        )}
      </div>

      {quota && (
        <div className="card" style={{marginTop: 16}}>
          <div className="card-head"><h3>Project quota</h3></div>
          <table className="table">
            <thead><tr><th>Resource</th><th>Used</th><th>Limit</th></tr></thead>
            <tbody>
            {quota.gpuClasses?.map(g => (
              <tr key={g.class}>
                <td className="mono">GPUs ({g.class})</td>
                <td>{g.used}</td>
                <td>{g.quota}</td>
              </tr>
            ))}
            <tr>
              <td className="mono">Volume storage</td>
              <td>{quota.storageUsed}</td>
              <td>{quota.storage}</td>
            </tr>
            <tr>
              <td className="mono">Volumes</td>
              <td>{quota.volumes}</td>
              <td>{quota.volumeMax || '—'}</td>
            </tr>
            <tr>
              <td className="mono">Pods</td>
              <td>{quota.pods}</td>
              <td>{quota.podMax || '—'}</td>
            </tr>
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
