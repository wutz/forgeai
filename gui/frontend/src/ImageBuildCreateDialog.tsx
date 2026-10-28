import {FormEvent, useState} from 'react'
import {CreateImageBuild} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText, generateName, NAME_MAX, NAME_PATTERN} from './util'
import {CancelButton, Dialog, Segmented} from './ui'
import {t} from './i18n'

// The forms the backend accepts for the pushed image and its tags.
const REPOSITORY_PATTERN = /^[a-z0-9]+([._-][a-z0-9]+)*(\/[a-z0-9]+([._-][a-z0-9]+)*)*$/
const TAG_PATTERN = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/
const TAGS_MAX = 8
const REPOSITORY_MAX = 200
const URL_PATTERN = /^https:\/\/\S+$/
const URL_MAX = 512
const PATH_MAX = 255
const DOCKERFILE_MAX = 65536

type Source = 'git' | 'dockerfile'

// parseTags splits "a, b c" into tags; empty entries are dropped.
const parseTags = (text: string) => text.split(/[\s,]+/).filter(Boolean)

// pathOK accepts a path inside the repository: relative, without ".." parts.
// An empty path is valid and takes the backend default.
const pathOK = (p: string) => !p.startsWith('/') && !p.split('/').includes('..')

// ImageBuildCreateDialog is the "New build" form: the image name and tags in
// the project registry, and the source the image is built from.
export function ImageBuildCreateDialog({onClose, onCreated, registry}: {
  onClose: () => void, onCreated: (name: string) => void, registry: string
}) {
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState(() => generateName('build'))
  const [repository, setRepository] = useState('')
  const [tagsText, setTagsText] = useState('latest')
  const [source, setSource] = useState<Source>('git')
  const [url, setURL] = useState('')
  const [revision, setRevision] = useState('')
  const [contextPath, setContextPath] = useState('.')
  const [dockerfilePath, setDockerfilePath] = useState('Dockerfile')
  const [dockerfile, setDockerfile] = useState('')

  const nameValue = name.trim()
  const nameOK = nameValue === '' || NAME_PATTERN.test(nameValue)
  const repo = repository.trim()
  const repoOK = REPOSITORY_PATTERN.test(repo)
  const tags = parseTags(tagsText)
  const tagsOK = tags.length >= 1 && tags.length <= TAGS_MAX && tags.every(tag => TAG_PATTERN.test(tag))
    && new Set(tags).size === tags.length
  const urlValue = url.trim()
  const urlOK = URL_PATTERN.test(urlValue)
  const contextValue = contextPath.trim()
  const dockerfilePathValue = dockerfilePath.trim()
  const pathsOK = pathOK(contextValue) && pathOK(dockerfilePathValue)
  const dockerfileOK = dockerfile.trim() !== ''
  // The footer names the first blocker, so a disabled Create is never unexplained.
  const blocker = !nameOK ? t('builds.create.blockerName')
    : !repoOK ? t('builds.create.blockerRepository')
    : !tagsOK ? t('builds.create.blockerTags')
    : source === 'git' && !urlOK ? t('builds.create.blockerUrl')
    : source === 'git' && !pathsOK ? t('builds.create.blockerPath')
    : source === 'dockerfile' && !dockerfileOK ? t('builds.create.blockerDockerfile')
    : ''
  const canSubmit = !busy && blocker === ''

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!canSubmit) return
    setBusy(true)
    const common = {name: nameValue || generateName('build'), repository: repo, tags}
    const request = source === 'git'
      ? app.ImageBuildRequest.createFrom({
        ...common,
        dockerfile: '',
        git: {url: urlValue, revision: revision.trim(), contextPath: contextValue, dockerfilePath: dockerfilePathValue},
      })
      : app.ImageBuildRequest.createFrom({...common, dockerfile})
    CreateImageBuild(request).then(onCreated, e => {
      setError(errText(e))
      setBusy(false)
    })
  }

  return (
    <Dialog title={t('builds.create.title')} subtitle={t('builds.create.subtitle')}
            onClose={onClose} busy={busy} footer={<>
              {blocker && !busy && <span className="hint hint-error" role="status">{blocker}</span>}
              <CancelButton disabled={busy}/>
              <button type="submit" form="image-build-form" className="btn" disabled={!canSubmit}>
                {busy ? t('builds.create.submitting') : t('builds.create.submit')}
              </button>
            </>}>
        <form id="image-build-form" onSubmit={submit} className="dialog-form">
          {error && <div className="banner banner-error" role="alert">{error}</div>}
          <div className="field">
            <label htmlFor="b-name">{t('builds.create.name')}</label>
            <input id="b-name" className="input mono" value={name} maxLength={NAME_MAX} disabled={busy}
                   onChange={e => setName(e.target.value)} aria-invalid={!nameOK}/>
            <span className="hint">{t('builds.create.nameHint')}</span>
          </div>
          <div className="field">
            <label htmlFor="b-repository">{t('builds.create.repository')}</label>
            <input id="b-repository" className="input mono" value={repository} placeholder="team/app"
                   maxLength={REPOSITORY_MAX} disabled={busy} onChange={e => setRepository(e.target.value)}
                   aria-invalid={!!repo && !repoOK}/>
            <span className="hint">{t('builds.create.repositoryHint', {registry})}</span>
          </div>
          <div className="field">
            <label htmlFor="b-tags">{t('builds.create.tags')}</label>
            <input id="b-tags" className="input mono" value={tagsText} placeholder="latest, v1.2" disabled={busy}
                   onChange={e => setTagsText(e.target.value)} aria-invalid={tagsText.trim() !== '' && !tagsOK}/>
            <span className="hint">{t('builds.create.tagsHint', {max: TAGS_MAX})}</span>
          </div>
          <div className="field">
            <label>{t('builds.create.source')}</label>
            <Segmented<Source> options={[
              {value: 'git', label: t('builds.sourceGit')},
              {value: 'dockerfile', label: t('builds.sourceDockerfile')},
            ]} value={source} onChange={setSource}/>
          </div>
          {source === 'git' ? <>
            <div className="field">
              <label htmlFor="b-url">{t('builds.create.url')}</label>
              <input id="b-url" className="input mono" value={url} placeholder="https://github.com/acme/app.git"
                     maxLength={URL_MAX} disabled={busy} onChange={e => setURL(e.target.value)}
                     aria-invalid={!!urlValue && !urlOK}/>
              <span className="hint">{t('builds.create.urlHint')}</span>
            </div>
            <div className="field">
              <label htmlFor="b-revision">{t('builds.create.revision')}</label>
              <input id="b-revision" className="input mono" value={revision} placeholder="HEAD" maxLength={PATH_MAX}
                     disabled={busy} onChange={e => setRevision(e.target.value)}/>
              <span className="hint">{t('builds.create.revisionHint')}</span>
            </div>
            <div className="field">
              <label htmlFor="b-context">{t('builds.create.contextPath')}</label>
              <input id="b-context" className="input mono" value={contextPath} maxLength={PATH_MAX} disabled={busy}
                     onChange={e => setContextPath(e.target.value)} aria-invalid={!pathOK(contextValue)}/>
              <span className="hint">{t('builds.create.contextPathHint')}</span>
            </div>
            <div className="field">
              <label htmlFor="b-dockerfile-path">{t('builds.create.dockerfilePath')}</label>
              <input id="b-dockerfile-path" className="input mono" value={dockerfilePath} maxLength={PATH_MAX}
                     disabled={busy} onChange={e => setDockerfilePath(e.target.value)}
                     aria-invalid={!pathOK(dockerfilePathValue)}/>
              <span className="hint">{t('builds.create.dockerfilePathHint')}</span>
            </div>
          </> : (
            <div className="field">
              <label htmlFor="b-dockerfile">{t('builds.create.dockerfile')}</label>
              <textarea id="b-dockerfile" className="textarea mono" rows={10} value={dockerfile} maxLength={DOCKERFILE_MAX}
                        disabled={busy} placeholder={'FROM alpine:3.20\nCMD ["echo", "hello"]'}
                        onChange={e => setDockerfile(e.target.value)}/>
              <span className="hint">{t('builds.create.dockerfileHint')}</span>
            </div>
          )}
        </form>
      </Dialog>
  )
}
