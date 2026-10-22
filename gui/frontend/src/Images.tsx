import {useEffect, useState} from 'react'
import {DeleteImage, DeleteImageRepo, DeleteImageTag, ListImages, Registry} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, fmtDate, tagCount} from './util'
import {t} from './i18n'
import {Badge, Card, Empty, Icon, PageHeader, RefreshButton, useActive, useConfirm} from './ui'

// countImages and countTags name a count in a sentence, with the singular
// form of one ("1 image") as the messages do for jobs.
const countImages = (n: number) => n === 1 ? t('images.imagesOne') : t('images.imagesMany', {count: n})
const countTags = (n: number) => n === 1 ? t('images.tagsOne') : t('images.tagsMany', {count: n})

// Images shows the project registry: the push command with the robot
// credentials and the pushed tags with delete buttons.
export function Images() {
  const [repos, setRepos] = useState<app.ImageRepo[]>([])
  // reg stays null until Registry answers, so the empty states do not flash.
  const [reg, setReg] = useState<app.Registry | null>(null)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const confirm = useConfirm()
  const active = useActive()

  const refresh = () => Promise.all([
    Registry().then(setReg),
    ListImages().then(setRepos),
  ]).then(() => setError(''), e => {
    setError(errText(e))
    throw e
  })
  useEffect(() => { if (active) refresh().catch(() => {}) }, [active])

  const copy = (text: string, key: string) =>
    navigator.clipboard.writeText(text).then(() => {
      setCopied(key)
      setTimeout(() => setCopied(''), 1500)
    }, () => {})

  // The password is not part of the command: docker asks for it, so it stays
  // out of shell history and the process list. The robot name contains "$",
  // which the shell would expand, so the username is single-quoted.
  const host = reg?.url.split('/')[0] ?? ''
  const login = reg?.username ? `docker login ${host} -u '${reg.username}'` : ''
  const address = (repo: string, tag: string) => reg ? `${reg.url}/${repo}:${tag}` : ''
  const totalTags = repos.reduce((n, r) => n + tagCount(r), 0)
  const totalPulls = repos.reduce((n, r) => n + r.pulls, 0)

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
                  meta={repos.length > 0 && <span className="muted" style={{fontSize: 12}}>
                    {t('images.summary', {repos: repos.length, tags: totalTags, pulls: totalPulls})}
                  </span>}
                  actions={<RefreshButton onRefresh={refresh}/>}/>
      {error && <div className="banner banner-error" role="alert">{error}</div>}
      {confirm.dialog}

      {reg?.enabled && login && (
        <Card title={t('images.pushTitle')} subtitle={t('images.pushSubtitle')}
              actions={<button className="btn btn-sm" onClick={() => copy(login, 'login')}>
                <Icon name={copied === 'login' ? 'check' : 'copy'} size={13}/>
                {copied === 'login' ? t('common.copied') : t('images.copyLogin')}
              </button>}>
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
      )}

      <Card title={t('images.repositories')} className="card-spaced">
        {reg?.enabled && !reg.credentials && (
          <Empty icon="shield" title={t('images.viewOnlyTitle')}>
            {t('images.viewOnlyHint')}
          </Empty>
        )}
        {repos.length === 0 && !error && reg?.enabled && reg.credentials && (
          <Empty icon="image" title={t('images.noneTitle')}>
            {t('images.noneHintBefore')} <code className="mono">{reg?.url}</code>{t('images.noneHintAfter')}
          </Empty>
        )}
        {repos.length === 0 && reg && !reg.enabled && (
          <Empty icon="image" title={t('images.notConfigured')}>
            {t('images.notConfiguredHint')}
          </Empty>
        )}
        {repos.map(r => (
          <details key={r.name} className="repo">
            <summary>
              <span className="mono">{reg?.url}/{r.name}</span>
              {r.error
                ? <Badge tone="error">{t('images.notListed')}</Badge>
                : <Badge>{t('images.tagsCount', {count: tagCount(r)})}</Badge>}
              <span className="muted" style={{fontSize: 12}}>{t('images.pullsCount', {count: r.pulls})}</span>
              <button className="btn btn-sm btn-danger" onClick={e => {
                e.preventDefault()
                deleteRepo(r)
              }}>{t('images.deleteRepo')}</button>
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
                  <td>
                    {tags.length
                      ? <div style={{display: 'flex', flexWrap: 'wrap', gap: 6}}>
                        {tags.map(tag => {
                          const key = `address-${img.digest}-${tag}`
                          return (
                            <span key={tag} style={{display: 'inline-flex', gap: 2}}>
                              <button className="btn btn-sm mono" title={t('images.copyAddress')}
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
                  <td className="mono trunc" title={img.digest}>{img.digest.slice(7, 19)}</td>
                  <td>{img.sizeMiB ? `${img.sizeMiB} MiB` : '—'}</td>
                  <td className="muted">{fmtDate(img.pushedAt)}</td>
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
        ))}
      </Card>
    </div>
  )
}
