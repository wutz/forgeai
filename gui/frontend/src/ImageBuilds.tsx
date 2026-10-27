import {useEffect, useState} from 'react'
import {DeleteImageBuild, ListImageBuilds, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, everyWhileVisible, fmtDate, Status, useCopy} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, HintButton, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'
import {ImageBuildCreateDialog} from './ImageBuildCreateDialog'
import {ImageBuildLogsDialog} from './ImageBuildLogsDialog'

// ImageBuilds builds images from a Dockerfile or a git repository and pushes them
// to the project registry. It sits under Images in the sidebar and is always
// there; a cluster without the ImageBuild CRD says so instead of listing builds.
// available is false on such a cluster.
export function ImageBuilds({available}: {available: boolean}) {
  // reg stays null until Registry answers, so the card does not show its empty states while it loads.
  const [reg, setReg] = useState<app.Registry | null>(null)
  // builds is null until the first list answers.
  const [builds, setBuilds] = useState<app.ImageBuild[] | null>(null)
  const [error, setError] = useState('')
  const {copied, copy} = useCopy(() => setError(t('images.copyFailed')))
  const confirm = useConfirm()
  const active = useActive()
  const [creating, setCreating] = useState(false)
  const [logsOf, setLogsOf] = useState('')

  const refresh = () => Promise.all([
    Registry().then(setReg),
    available ? ListImageBuilds().then(setBuilds) : Promise.resolve(),
  ]).then(() => setError(''), e => {
    setError(errText(e))
    throw e
  })
  // A running build is read every few seconds so its status shows without a reload; otherwise the page's slower beat.
  const building = builds?.some(b => b.phase === 'Pending' || b.phase === 'Running') ?? false
  useEffect(() => {
    if (!active) return
    const poll = () => refresh().catch(() => {})
    poll()
    return everyWhileVisible(poll, building ? 5000 : 60000)
  }, [active, building])

  // Listing needs read access only; starting a build needs the robot credentials.
  const canBuild = !!reg?.enabled && !!reg.credentials

  const deleteBuild = (b: app.ImageBuild) => confirm.ask({
    title: t('builds.deleteTitle', {name: b.name}),
    message: t('builds.deleteMessage'),
    action: t('common.delete'), danger: true,
    run: () => DeleteImageBuild(b.name).then(refresh).catch(e => setError(errText(e))),
  })

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('builds.title')}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}
      {creating && reg && <ImageBuildCreateDialog registry={reg.url} onClose={() => setCreating(false)}
        onCreated={() => {
          setCreating(false)
          refresh().catch(() => {})
        }}/>}
      {logsOf && <ImageBuildLogsDialog name={logsOf} running={!!builds?.some(b => b.name === logsOf && (b.phase === 'Pending' || b.phase === 'Running'))}
        onClose={() => setLogsOf('')}/>}

      {!available && (
        <Card>
          <Empty icon="image" title={t('builds.unavailableTitle')}>
            {t('builds.unavailableHint')}
          </Empty>
        </Card>
      )}

      {available && reg === null && !error && <Spinner label={t('common.loading')}/>}
      {available && reg && (
        <Card title={t('builds.title')} subtitle={t('builds.subtitle')} className="card-spaced"
              actions={<HintButton className="btn btn-sm" disabled={!canBuild}
                                  reason={reg.enabled ? t('builds.needCredentials') : t('builds.needRegistry')}
                                  onClick={() => setCreating(true)}>
                <Icon name="plus" size={13}/>{t('builds.new')}
              </HintButton>}>
          {!reg.enabled && <Empty icon="image" title={t('builds.needRegistry')}/>}
          {reg.enabled && <>
            {builds === null && !error && <Spinner label={t('common.loading')}/>}
            {builds !== null && builds.length === 0 && <Empty icon="image" title={t('builds.noneTitle')}>
              {canBuild && t('builds.noneHint')}
            </Empty>}
            {builds !== null && builds.length > 0 && <table className="table">
              <thead><tr>
                <th>{t('builds.name')}</th><th>{t('builds.image')}</th><th>{t('builds.source')}</th>
                <th>{t('common.status')}</th><th>{t('builds.pushed')}</th><th>{t('common.created')}</th><th/>
              </tr></thead>
              <tbody>
              {builds.map(b => (
                <tr key={b.name}>
                  <td data-label={t('builds.name')} className="mono trunc" title={b.name}>{b.name}</td>
                  <td data-label={t('builds.image')}>
                    <div style={{display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center'}}>
                      <span className="mono">{b.repository}</span>
                      {b.tags.map(tag => <Badge key={tag}>{tag}</Badge>)}
                    </div>
                  </td>
                  <td data-label={t('builds.source')} className="muted">
                    {b.source === 'git' ? t('builds.sourceGit') : t('builds.sourceDockerfile')}
                  </td>
                  <td data-label={t('common.status')}>
                    <Status phase={b.phase}/>
                    {b.phase === 'Failed' && b.message && <div className="hint hint-error">{b.message}</div>}
                  </td>
                  <td data-label={t('builds.pushed')}>
                    {b.images?.length
                      ? <div style={{display: 'flex', flexWrap: 'wrap', gap: 6}}>
                        {b.images.map(image => {
                          const key = `build-${b.name}-${image}`
                          // The name is shown without the project registry prefix; the full address is copied.
                          const short = reg.url ? image.replace(`${reg.url}/`, '') : image
                          return (
                            <button key={image} className="btn btn-sm mono" title={image}
                                    aria-label={`${t('builds.copyImage')}: ${image}`}
                                    onClick={() => copy(image, key)}>
                              <Icon name={copied === key ? 'check' : 'copy'} size={12}/>{short}
                            </button>
                          )
                        })}
                      </div>
                      : <span className="muted">{t('builds.notPushed')}</span>}
                  </td>
                  <td data-label={t('common.created')} className="muted">{fmtDate(b.createdAt)}</td>
                  <td>
                    <div className="actions">
                      <button className="btn btn-sm" onClick={() => setLogsOf(b.name)}>{t('common.logs')}</button>
                      <button className="btn btn-sm btn-danger" onClick={() => deleteBuild(b)}>{t('common.delete')}</button>
                    </div>
                  </td>
                </tr>
              ))}
              </tbody>
            </table>}
          </>}
        </Card>
      )}
    </div>
  )
}
