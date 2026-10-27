import {useEffect, useMemo, useRef, useState} from 'react'
import {DeleteImageBuild, ImageBuildLogs, ListImageBuilds, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, everyWhileVisible, fmtDate, Status, useCopy} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, HintButton, Icon, LogView, PageHeader, RefreshButton, Segmented, Spinner, Tabs, tabPanelProps, useActive, useConfirm, useDetailClose} from './ui'
import {ImageBuildCreateDialog} from './ImageBuildCreateDialog'

type BuildBucket = 'running' | 'queued' | 'failed' | 'done'
type Filter = 'all' | BuildBucket
type DetailTab = 'overview' | 'log'

// LOG_LINES is how many of the last lines of the build pod the log tab reads.
const LOG_LINES = 500
// LOG_POLL_MS is how often the log is read again while the build is still running.
const LOG_POLL_MS = 5000

// isBuilding reports a build that is still working, so the lists poll faster until it passes.
const isBuilding = (phase: string) => phase === 'Pending' || phase === 'Running'

// buildBucket groups a build phase for the filters. A build with no phase yet counts as queued.
function buildBucket(phase: string): BuildBucket {
  switch (phase) {
    case 'Running': return 'running'
    case 'Failed': return 'failed'
    case 'Succeeded': return 'done'
    default: return 'queued'
  }
}

// sourceLabel is the text for where a build takes its source from.
function sourceLabel(source: string): string {
  return source === 'git' ? t('builds.sourceGit') : t('builds.sourceDockerfile')
}

// filterOptions is built per render so the labels follow the language.
const filterOptions = (): {value: Filter, label: string}[] => [
  {value: 'all', label: t('builds.filterAll')},
  {value: 'running', label: t('builds.filterRunning')},
  {value: 'queued', label: t('builds.filterQueued')},
  {value: 'failed', label: t('builds.filterFailed')},
  {value: 'done', label: t('builds.filterDone')},
]

// ImageBuilds builds images from a Dockerfile or a git repository and pushes them
// to the project registry. It sits under Images in the sidebar and is always
// there; a cluster without the ImageBuild CRD says so instead of listing builds.
// available is false on such a cluster.
export function ImageBuilds({available}: {available: boolean}) {
  // reg stays null until Registry answers, so the page does not show its empty states while it loads.
  const [reg, setReg] = useState<app.Registry | null>(null)
  // builds is null until the first list answers, so a failed first read is not shown as an empty list.
  const [builds, setBuilds] = useState<app.ImageBuild[] | null>(null)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState<Filter>('all')
  const [creating, setCreating] = useState(false)
  const {split, close: closeDetail} = useDetailClose(selected, setSelected)
  const active = useActive()
  // Each refresh takes a number; a response that is not from the latest refresh is dropped,
  // so a slow older poll cannot overwrite a newer list.
  const latest = useRef(0)

  // refresh rejects when a read fails; the banner shows why and the header's refresh button reports it.
  const refresh = () => {
    const seq = ++latest.current
    const current = () => seq === latest.current
    return Promise.all([
      Registry().then(r => current() && setReg(r)),
      available ? ListImageBuilds().then(list => current() && setBuilds(list)) : null,
    ]).then(() => current() && setError(''), e => {
      if (current()) setError(errText(e))
      throw e
    })
  }
  // reload is for the callers with nowhere to show a rejection; the failure is already in the banner.
  const reload = () => refresh().catch(() => undefined)

  const list = builds ?? []
  const building = list.some(b => isBuilding(b.phase))
  useEffect(() => {
    if (!active || !available) return
    reload()
    return everyWhileVisible(reload, building ? 1500 : 5000)
  }, [active, available, building])

  const counts = useMemo(() => {
    const c: Record<Filter, number> = {all: 0, running: 0, queued: 0, failed: 0, done: 0}
    for (const b of builds ?? []) {
      c.all++
      c[buildBucket(b.phase)]++
    }
    return c
  }, [builds])

  const shown = builds && (filter === 'all' ? builds : builds.filter(b => buildBucket(b.phase) === filter))

  // A filter that hides the open build closes its detail with it: the detail is never for a row the list does not show.
  const chooseFilter = (next: Filter) => {
    setFilter(next)
    const open = list.find(b => b.name === selected)
    if (open && next !== 'all' && buildBucket(open.phase) !== next) setSelected(null)
  }

  // A build that leaves the list, deleted elsewhere, closes its detail.
  useEffect(() => {
    if (builds && selected && !builds.some(b => b.name === selected)) setSelected(null)
  }, [builds, selected])

  // Listing needs read access only; starting a build needs the robot credentials.
  const canBuild = !!reg?.enabled && !!reg.credentials
  const open = list.find(b => b.name === selected)

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('builds.title')}
                  meta={<span className="muted" style={{fontSize: 12.5}}>{builds !== null && t('builds.total', {count: builds.length})}</span>}
                  actions={<>
                    {available && <HintButton className="btn" disabled={!canBuild}
                                              reason={reg && !reg.enabled ? t('builds.needRegistry') : t('builds.needCredentials')}
                                              onClick={() => setCreating(true)}>
                      <Icon name="plus" size={14}/>{t('builds.new')}
                    </HintButton>}
                    <RefreshButton onRefresh={refresh}/>
                  </>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {creating && reg && <ImageBuildCreateDialog registry={reg.url} onClose={() => setCreating(false)} onCreated={name => {
        setCreating(false)
        // A new build is pending, so a filter that is not All may hide it; the list goes back to All with it open.
        setFilter('all')
        reload().then(() => setSelected(name))
      }}/>}

      {!available && (
        <Card>
          <Empty icon="image" title={t('builds.unavailableTitle')}>
            {t('builds.unavailableHint')}
          </Empty>
        </Card>
      )}
      {available && reg === null && !error && <Card><Spinner label={t('common.loading')}/></Card>}
      {available && reg && !reg.enabled && <Card><Empty icon="image" title={t('builds.needRegistry')}/></Card>}

      {available && reg?.enabled && <>
        <div className="list-toolbar">
          {/* Counts are left out until the list is read: zeros would say the project has no builds. */}
          <Segmented options={filterOptions()} value={filter} onChange={chooseFilter} counts={builds ? counts : undefined}/>
        </div>

        <div className="split" ref={split}>
          <BuildTable builds={shown} failed={!!error} filtered={filter !== 'all'} canBuild={canBuild} selected={selected}
                      onSelect={n => setSelected(prev => prev === n ? null : n)}/>
          {/* Keyed by name: a read still in flight for the previous build lands on an unmounted detail. */}
          {open && <BuildDetail key={open.name} build={open} registry={reg.url}
                                onDeleted={() => { setSelected(null); reload() }} onClose={closeDetail}/>}
        </div>
      </>}
    </div>
  )
}

function BuildTable({builds, failed, filtered, canBuild, selected, onSelect}: {
  builds: app.ImageBuild[] | null, failed: boolean, filtered: boolean, canBuild: boolean,
  selected: string | null, onSelect: (name: string) => void
}) {
  // A failed first read leaves nothing to list, so the card is left out under the banner.
  if (!builds) return failed ? null : <Card><Spinner label={t('common.loading')}/></Card>
  if (!builds.length) {
    return <Card>{filtered
      ? <Empty icon="image" title={t('builds.emptyFiltered')}/>
      : <Empty icon="image" title={t('builds.noneTitle')}>{canBuild && t('builds.noneHint')}</Empty>}</Card>
  }
  return (
    <Card>
      {/* Beside an open build the table keeps its status and image columns on one line and drops
          the created column: the detail shows when the build was created. */}
      <table className={selected ? 'table split-table' : 'table'}>
        <thead>
        <tr>
          <th>{t('builds.name')}</th>
          <th>{t('common.status')}</th>
          <th>{t('builds.image')}</th>
          {!selected && <th>{t('common.created')}</th>}
        </tr>
        </thead>
        <tbody>
        {builds.map(b => (
          <tr key={b.name} className={selected === b.name ? 'clickable selected' : 'clickable'}
              onClick={() => onSelect(b.name)}>
            <td data-label={t('builds.name')} className="mono">
              <button type="button" className="row-name cell-main">{b.name}</button>
              <span className="cell-sub">{sourceLabel(b.source)}</span>
            </td>
            <td data-label={t('common.status')} className="nowrap">
              <Status phase={b.phase}/>
              {b.phase === 'Failed' && b.message && <div className="cell-sub" title={b.message}>{b.message}</div>}
            </td>
            <td data-label={t('builds.image')}>
              <div style={{display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center'}}>
                <span className="mono">{b.repository}</span>
                {(b.tags ?? []).map(tag => <Badge key={tag}>{tag}</Badge>)}
              </div>
            </td>
            {!selected && <td data-label={t('common.created')} className="muted">{fmtDate(b.createdAt)}</td>}
          </tr>
        ))}
        </tbody>
      </table>
    </Card>
  )
}

function BuildDetail({build, registry, onDeleted, onClose}: {
  build: app.ImageBuild, registry: string, onDeleted: () => void, onClose: () => void
}) {
  const [tab, setTab] = useState<DetailTab>('overview')
  // error is an action's failure; logError is a failed log read. Each clears only its own.
  const [error, setError] = useState('')
  const [logError, setLogError] = useState('')
  // null until the first read, so the log view can tell "reading" from "no output".
  const [log, setLog] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const {copied, copy} = useCopy(() => setError(t('images.copyFailed')))
  const confirm = useConfirm()
  const active = useActive()

  // readLog reads the last lines of the build pod; a failure is shown in the log tab.
  const readLog = () => ImageBuildLogs(build.name, LOG_LINES).then(text => {
    setLog(text)
    setLogError('')
  }, e => setLogError(errText(e)))

  // The log is read when its tab opens and again while the build runs.
  const live = isBuilding(build.phase)
  useEffect(() => {
    if (tab !== 'log' || !active) return
    readLog()
    return live ? everyWhileVisible(readLog, LOG_POLL_MS) : undefined
  }, [tab, active, live])

  const remove = () => confirm.ask({
    title: t('builds.deleteTitle', {name: build.name}),
    message: t('builds.deleteMessage'),
    action: t('common.delete'), danger: true,
    run: () => {
      setDeleting(true)
      setError('')
      DeleteImageBuild(build.name).then(onDeleted).catch(e => setError(errText(e))).finally(() => setDeleting(false))
    },
  })

  return (
    <Card className="detail">
      <div className="card-head">
        <div className="detail-title">
          <h2 className="mono">{build.name}</h2>
          <div className="card-sub">{t('builds.detailSub', {
            source: sourceLabel(build.source), created: fmtDate(build.createdAt),
          })}</div>
        </div>
        <div className="card-head-actions">
          <Status phase={build.phase}/>
          <button type="button" className="icon-btn" aria-label={t('common.close')} title={t('common.close')} onClick={onClose}>
            <Icon name="close" size={14}/>
          </button>
        </div>
      </div>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      <div className="detail-actions">
        <span className="spacer"/>
        <button className="btn btn-sm btn-danger" disabled={deleting} onClick={remove}>
          <Icon name="trash" size={13}/>{deleting ? t('jobs.deleting') : t('common.delete')}
        </button>
      </div>

      <div className="section">
        <Tabs idPrefix="build" tabs={[
          {value: 'overview', label: t('builds.tabOverview'), icon: 'grid'},
          {value: 'log', label: t('builds.tabLog'), icon: 'scroll'},
        ]} value={tab} onChange={setTab}/>

        <div {...tabPanelProps('build', tab)}>
          {tab === 'overview' && (
            <dl className="kv">
              <dt>{t('common.image')}</dt><dd>
                <span className="mono">{build.repository}</span>
                {(build.tags ?? []).map(tag => <Badge key={tag}>{tag}</Badge>)}
              </dd>
              <dt>{t('builds.source')}</dt><dd>{sourceLabel(build.source)}</dd>
              <dt>{t('builds.digest')}</dt><dd className="mono">{build.digest || '—'}</dd>
              <dt>{t('builds.pushed')}</dt><dd>
                {(build.images ?? []).length
                  ? <div style={{display: 'flex', flexWrap: 'wrap', gap: 6}}>
                    {(build.images ?? []).map(image => {
                      const key = `build-${image}`
                      // The name is shown without the project registry prefix; the full address is copied.
                      const short = registry ? image.replace(`${registry}/`, '') : image
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
              </dd>
              {build.phase === 'Failed' && build.message && <>
                <dt>{t('builds.failure')}</dt><dd>{build.message}</dd>
              </>}
              <dt>{t('builds.started')}</dt><dd>{fmtDate(build.startedAt)}</dd>
              <dt>{t('builds.completed')}</dt><dd>{fmtDate(build.completedAt)}</dd>
              <dt>{t('common.created')}</dt><dd>{fmtDate(build.createdAt)}</dd>
            </dl>
          )}

          {tab === 'log' && <>
            {logError && <div className="banner banner-error" role="alert">{logError}</div>}
            {/* With no log read yet and a read that failed, the log view would wait on that read; retry offers it again. */}
            {log === null && logError
              ? <div className="actions"><button className="btn btn-sm" onClick={readLog}>{t('common.retry')}</button></div>
              : <LogView text={log} onRefresh={readLog}/>}
          </>}
        </div>
      </div>
    </Card>
  )
}
