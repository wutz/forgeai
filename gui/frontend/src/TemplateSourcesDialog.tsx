import {FormEvent, useEffect, useState} from 'react'
import {CatalogSources, CheckCatalogSource} from '../wailsjs/go/app/App'
import {app} from '../wailsjs/go/models'
import {errText} from './util'
import {t} from './i18n'
import {CancelButton, Dialog} from './ui'
import {loadTemplateSources, saveTemplateSources, sameSource} from './templateSources'

// TemplateSourcesDialog lists the template sources the create dialogs read, and
// adds or removes the ones the user added on this machine. The platform's source
// is listed too, but it is named by the platform config and stays.
export function TemplateSourcesDialog({onClose}: {onClose: () => void}) {
  const [sources, setSources] = useState<app.CatalogSource[]>([])
  const [loadError, setLoadError] = useState('')
  const [location, setLocation] = useState('')
  const [checking, setChecking] = useState(false)
  const [error, setError] = useState('')
  const [note, setNote] = useState('')

  // reload reads every source, so each row shows its own status.
  const reload = () => {
    CatalogSources(loadTemplateSources()).then(s => {
      setSources(s)
      setLoadError('')
    }, err => setLoadError(errText(err)))
  }
  useEffect(() => { reload() }, [])

  // add reads the source before it is saved: a source that cannot be read is
  // not added, so the create dialogs never list a source that fails.
  const add = (e: FormEvent) => {
    e.preventDefault()
    const loc = location.trim()
    setError('')
    setNote('')
    if (!loc || checking) return
    if (sources.some(s => sameSource(s.location, loc))) {
      setError(t('templateSources.alreadyAdded'))
      return
    }
    setChecking(true)
    CheckCatalogSource(loc).then(s => {
      if (s.error) {
        setError(t('templateSources.checkFailed', {error: s.error}))
        return
      }
      saveTemplateSources([...loadTemplateSources(), loc])
      setLocation('')
      setNote(t('templateSources.added', {name: s.displayName || s.location, count: s.templates.filter(x => !x.error).length}))
      reload()
    }, err => setError(errText(err))).finally(() => setChecking(false))
  }

  const remove = (loc: string) => {
    saveTemplateSources(loadTemplateSources().filter(s => !sameSource(s, loc)))
    setNote('')
    reload()
  }

  return (
    <Dialog title={t('templateSources.title')} subtitle={t('templateSources.subtitle')}
            onClose={onClose} footer={<CancelButton/>}>
      <div className="dialog-form">
        <p className="hint">{t('templateSources.trust')}</p>
        {loadError && <div className="banner banner-error" role="alert">{t('templateSources.loadFailed', {error: loadError})}</div>}
        {note && <div className="banner banner-success" role="status">{note}</div>}
        <table className="table">
          <thead>
            <tr>
              <th>{t('templateSources.colSource')}</th>
              <th>{t('templateSources.colTemplates')}</th>
              <th>{t('templateSources.colStatus')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {sources.map(s => {
              const usable = (s.templates || []).filter(x => !x.error).length
              return (
                <tr key={s.location}>
                  <td data-label={t('templateSources.colSource')}>
                    <span className="cell-main">{s.displayName || s.location}</span>
                    <span className="cell-sub mono">{s.location}</span>
                  </td>
                  <td data-label={t('templateSources.colTemplates')}>{s.error ? '—' : usable}</td>
                  <td data-label={t('templateSources.colStatus')}>
                    {s.platform
                      ? <span className="muted" title={t('templateSources.platformHint')}>{t('templateSources.platform')}</span>
                      : s.error
                        ? <span className="hint hint-error" role="status">{s.error}</span>
                        : <span className="muted">{t('templateSources.ready', {count: usable})}</span>}
                  </td>
                  <td className="actions">
                    {!s.platform && <button type="button" className="btn btn-sm btn-danger"
                                            aria-label={t('templateSources.removeLabel', {name: s.location})}
                                            onClick={() => remove(s.location)}>
                      {t('templateSources.remove')}
                    </button>}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
        {!sources.some(s => !s.platform) && <p className="hint">{t('templateSources.none')}</p>}
        <form onSubmit={add} className="field">
          <label htmlFor="ts-location">{t('templateSources.location')}</label>
          <div className="row">
            <input id="ts-location" className="input mono" value={location}
                   autoCapitalize="none" autoCorrect="off" spellCheck={false}
                   placeholder={t('templateSources.locationPlaceholder')}
                   onChange={e => setLocation(e.target.value)}/>
            <button type="submit" className="btn" disabled={checking || !location.trim()}>
              {checking ? t('templateSources.checking') : t('templateSources.add')}
            </button>
          </div>
          <span className="hint">{t('templateSources.locationHint')}</span>
          {error && <span className="hint hint-error" role="alert">{error}</span>}
        </form>
      </div>
    </Dialog>
  )
}
