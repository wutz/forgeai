import {useEffect, useState} from 'react'
import {DeleteImage, DeleteImageRepo, DeleteImageTag, ListImages, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, everyWhileVisible, fmtDate, tagCount, useCopy} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, Spinner, useActive, useConfirm} from './ui'

// countImages and countTags name a count in a sentence, with the singular
// form of one ("1 image") as the messages do for jobs.
const countImages = (n: number) => n === 1 ? t('images.imagesOne') : t('images.imagesMany', {count: n})
const countTags = (n: number) => n === 1 ? t('images.tagsOne') : t('images.tagsMany', {count: n})

// Images shows the project registry's repositories: the pushed tags with delete
// buttons. How to push is in ImagePush and how to build is in ImageBuilds, which
// sit under it in the sidebar.
export function Images() {
  // repos is null until the first list answers, so the empty state does not show while it loads.
  const [repos, setRepos] = useState<app.ImageRepo[] | null>(null)
  const list = repos ?? []
  // reg stays null until Registry answers, so the empty states do not flash.
  const [reg, setReg] = useState<app.Registry | null>(null)
  const [error, setError] = useState('')
  const {copied, copy} = useCopy(() => setError(t('images.copyFailed')))
  const confirm = useConfirm()
  const active = useActive()

  const refresh = () => Promise.all([
    Registry().then(setReg),
    ListImages().then(setRepos),
  ]).then(() => setError(''), e => {
    setError(errText(e))
    throw e
  })
  // Harbor is asked once per repository, so the page re-reads on the Overview's slower beat.
  useEffect(() => {
    if (!active) return
    const poll = () => refresh().catch(() => {})
    poll()
    return everyWhileVisible(poll, 60000)
  }, [active])

  const address = (repo: string, tag: string) => reg ? `${reg.url}/${repo}:${tag}` : ''
  const totalTags = list.reduce((n, r) => n + tagCount(r), 0)
  const totalPulls = list.reduce((n, r) => n + r.pulls, 0)
  // Repositories the registry could not list: their tags are not in the totals above.
  const unlisted = list.filter(r => r.error).length

  const deleteRepo = (r: app.ImageRepo) => confirm.ask({
    title: t('images.deleteRepoTitle', {name: r.name}),
    // A repository that failed to list has no known image count to show.
    message: r.error
      ? t('images.deleteRepoUnlisted')
      : t('images.deleteRepoMessage', {images: countImages(r.images?.length ?? 0), tags: countTags(tagCount(r))}),
    action: t('images.deleteRepo'), danger: true,
    run: () => DeleteImageRepo(r.name).then(refresh).catch(e => setError(errText(e))),
  })
  // Harbor deletes an image by digest, so every tag on it goes with the row.
  const deleteImage = (r: app.ImageRepo, img: app.Image) => {
    const tags = img.tags?.filter(Boolean) ?? []
    confirm.ask({
      title: t('images.deleteImageTitle', {digest: img.digest.slice(7, 19), name: r.name}),
      message: tags.length
        ? t('images.deleteImageTags', {tags: tags.join(', ')})
        : t('images.deleteImageMessage'),
      action: t('images.deleteImage'), danger: true,
      run: () => DeleteImage(r.name, img.digest).then(refresh).catch(e => setError(errText(e))),
    })
  }
  // A tag is removed alone: the image and its other tags stay.
  const deleteTag = (r: app.ImageRepo, img: app.Image, tag: string) => {
    const others = (img.tags?.filter(Boolean) ?? []).length - 1
    confirm.ask({
      title: t('images.deleteTagTitle', {tag, name: r.name}),
      message: others > 0 ? t('images.deleteTagMessage') : t('images.deleteTagLast'),
      action: t('images.deleteTag'), danger: true,
      run: () => DeleteImageTag(r.name, img.digest, tag).then(refresh).catch(e => setError(errText(e))),
    })
  }

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('images.title')}
                  meta={list.length > 0 && <span className="muted" style={{fontSize: 12}}>
                    {t('images.summary', {repos: list.length, tags: totalTags, pulls: totalPulls})}
                    {unlisted > 0 && <> · {t('images.unlistedCount', {count: unlisted})}</>}
                  </span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      {/* A failed first read leaves no list, so the card is left out rather than drawn empty under the banner. */}
      {(repos || !error) && <Card title={t('images.repositories')} className="card-spaced">
        {reg?.enabled && !reg.credentials && (
          <Empty icon="shield" title={t('images.viewOnlyTitle')}>
            {t('images.viewOnlyHint')}
          </Empty>
        )}
        {repos === null && !error && <Spinner label={t('common.loading')}/>}
        {repos !== null && list.length === 0 && !error && reg?.enabled && reg.credentials && (
          <Empty icon="image" title={t('images.noneTitle')}>
            {t('images.noneHintBefore')} <code className="mono">{reg?.url}</code>{t('images.noneHintAfter')}
          </Empty>
        )}
        {repos !== null && list.length === 0 && reg && !reg.enabled && (
          <Empty icon="image" title={t('images.notConfigured')}>
            {t('images.notConfiguredHint')}
          </Empty>
        )}
        {list.map(r => (
          <div key={r.name} className="repo-row">
            {/* A repository that could not be listed opens on its error, which is otherwise behind the summary. */}
            <details className="repo" open={!!r.error}>
              <summary>
                <span className="mono">{reg?.url}/{r.name}</span>
                {r.error
                  ? <span title={r.error}><Badge tone="error">{t('images.notListed')}</Badge></span>
                  : <Badge>{t('images.tagsCount', {count: tagCount(r)})}</Badge>}
                <span className="muted" style={{fontSize: 12}}>{t('images.pullsCount', {count: r.pulls})}</span>
              </summary>
              {r.error && <div className="banner banner-error" role="alert">
                {t('images.repoError', {error: r.error})}
              </div>}
              <table className="table">
                <thead><tr><th>{t('images.tag')}</th><th>{t('images.digest')}</th><th>{t('images.size')}</th><th>{t('images.pushed')}</th><th/></tr></thead>
                <tbody>
                {r.images?.map(img => {
                  const tags = img.tags?.filter(Boolean) ?? []
                  return (
                  <tr key={img.digest}>
                    <td data-label={t('images.tag')}>
                      {tags.length
                        ? <div style={{display: 'flex', flexWrap: 'wrap', gap: 6}}>
                          {tags.map(tag => {
                            const key = `address-${img.digest}-${tag}`
                          return (
                            <span key={tag} style={{display: 'inline-flex', gap: 2}}>
                              {/* The name carries the address, not only the tag: the same tag exists in several repositories. */}
                              <button className="btn btn-sm mono" title={t('images.copyAddress')}
                                      aria-label={`${t('images.copyAddress')}: ${address(r.name, tag)}`}
                                      onClick={() => copy(address(r.name, tag), key)}>
                                <Icon name={copied === key ? 'check' : 'copy'} size={12}/>{tag}
                              </button>
                              <button className="icon-btn" title={t('images.deleteTagLabel', {tag})}
                                      aria-label={t('images.deleteTagLabel', {tag})}
                                      onClick={() => deleteTag(r, img, tag)}>
                                <Icon name="trash" size={12}/>
                              </button>
                            </span>
                          )
                        })}
                      </div>
                      : <span className="muted">{t('images.untagged')}</span>}
                  </td>
                  <td data-label={t('images.digest')} className="mono trunc" title={img.digest}>{img.digest.slice(7, 19)}</td>
                  <td data-label={t('images.size')}>{img.sizeMiB ? `${img.sizeMiB} MiB` : '—'}</td>
                  <td data-label={t('images.pushed')} className="muted">{fmtDate(img.pushedAt)}</td>
                  <td>
                    <div className="actions">
                      <button className="btn btn-sm btn-danger" onClick={() => deleteImage(r, img)}>
                        {t('images.deleteImage')}
                      </button>
                    </div>
                  </td>
                </tr>
                )
              })}
              </tbody>
            </table>
            </details>
            {/* The delete is beside the disclosure, not in its summary: a button inside
                summary is invalid interactive content and Enter on it toggles the list. */}
            <button className="btn btn-sm btn-danger" onClick={() => deleteRepo(r)}>{t('images.deleteRepo')}</button>
          </div>
        ))}
      </Card>}
    </div>
  )
}

// ImagePush shows how to push images to the project registry: the robot login,
// its password and the project address, then the commands that tag, push and
// pull. The repositories already pushed are listed by Images.
export function ImagePush() {
  // reg stays null until Registry answers, so the cards do not show their empty states while it loads.
  const [reg, setReg] = useState<app.Registry | null>(null)
  const [error, setError] = useState('')
  const {copied, copy} = useCopy(() => setError(t('images.copyFailed')))
  const [showPassword, setShowPassword] = useState(false)
  const active = useActive()

  // refresh rejects when the read fails; the banner shows why and the header's refresh button reports it.
  const refresh = () => Registry().then(r => {
    setReg(r)
    setError('')
  }, e => {
    setError(errText(e))
    throw e
  })
  // reload is for the effect, which has nowhere to show a rejection; the banner already shows it.
  const reload = () => refresh().catch(() => undefined)
  useEffect(() => {
    if (!active) return
    reload()
  }, [active])

  // The password is not part of the command: docker asks for it, so it stays
  // out of shell history and the process list. The robot name contains "$",
  // which the shell would expand, so the username is single-quoted.
  const host = reg?.url.split('/')[0] ?? ''
  const login = reg?.username ? `docker login ${host} -u '${reg.username}'` : ''

  return (
    <div className="page">
      <PageHeader eyebrow={t('storage.eyebrow')} title={t('images.pushTitle')}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {reg === null && !error && <Spinner label={t('common.loading')}/>}
      {reg && !reg.enabled && (
        <Card>
          <Empty icon="image" title={t('images.notConfigured')}>
            {t('images.notConfiguredHint')}
          </Empty>
        </Card>
      )}
      {reg?.enabled && !reg.credentials && (
        <Card>
          <Empty icon="shield" title={t('images.viewOnlyTitle')}>
            {t('images.viewOnlyHint')}
          </Empty>
        </Card>
      )}

      {reg?.enabled && login && (
        <>
          <p className="muted" style={{margin: '0 0 12px'}}>{t('images.pushSubtitle')}</p>
          <Card>
            <pre className="code" style={{margin: 0, padding: 14, borderRadius: 0, border: 0}}>{login}</pre>
            <div className="robot-password">
              <span className="muted">{t('images.password')}</span>
              <code className="mono">{showPassword ? reg.password : '••••••••••••'}</code>
              <button className="btn btn-sm" onClick={() => setShowPassword(v => !v)}>
                {showPassword ? t('images.hide') : t('images.show')}
              </button>
              <button className="btn btn-sm" onClick={() => copy(reg.password ?? '', 'password')}>
                <Icon name={copied === 'password' ? 'check' : 'copy'} size={13}/>
                {copied === 'password' ? t('common.copied') : t('images.copyPassword')}
              </button>
            </div>
            <div className="robot-password">
              <span className="muted">{t('common.project')}</span>
              <code className="mono">{reg.url}</code>
              <button className="btn btn-sm" onClick={() => copy(reg.url, 'project')}>
                <Icon name={copied === 'project' ? 'check' : 'copy'} size={13}/>
                {copied === 'project' ? t('common.copied') : t('images.copyProject')}
              </button>
            </div>
            <div className="robot-password">
              <span className="muted">{t('images.pushThen')}</span>
              <pre className="code" style={{margin: 0, padding: 0, border: 0, background: 'none'}}>
                {`docker tag <image>:<tag> ${reg.url}/<repo>:<tag>\ndocker push ${reg.url}/<repo>:<tag>\ndocker pull ${reg.url}/<repo>:<tag>`}
              </pre>
            </div>
          </Card>
        </>
      )}
    </div>
  )
}
