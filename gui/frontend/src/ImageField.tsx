import {useEffect, useState} from 'react'
import {ListImages, Registry} from '../wailsjs/go/app/App'

// ImageField is the image input of the job and sandbox forms and their
// templates. Its suggestions are the tags pushed to the project registry (the
// Images view) and the platform's offered images; any other address can still
// be typed, so public or external images work too.
export function ImageField({id, value, onChange, offered, placeholder, required}: {
  id: string, value: string, onChange: (image: string) => void,
  // offered is null when the platform config lists no images (Go nil slice).
  offered?: string[] | null, placeholder?: string, required?: boolean,
}) {
  const [pushed, setPushed] = useState<string[]>([])
  const [loading, setLoading] = useState(true)

  // Registry and ListImages fail for view-only members and for projects
  // without a registry; the field then only offers the platform images.
  useEffect(() => {
    Promise.all([Registry(), ListImages()]).then(([reg, repos]) => {
      if (!reg.enabled) return
      setPushed(repos.flatMap(r => (r.images ?? []).flatMap(img =>
        (img.tags ?? []).filter(Boolean).map(tag => `${reg.url}/${r.name}:${tag}`)
      )).sort())
    }).catch(() => {}).finally(() => setLoading(false))
  }, [])

  const listId = `${id}-images`
  const options = [...new Set([...pushed, ...(offered ?? [])])]
  const hint = loading ? 'Loading the project images…'
    : pushed.length > 0 ? `Pick one of the ${pushed.length} project images (Images view) or type any address.`
    : 'No project images yet: push one in the Images view, or type any address.'

  return (
    <>
      <input id={id} className="input mono" value={value} list={listId}
             autoCapitalize="none" autoCorrect="off" spellCheck={false}
             onChange={e => onChange(e.target.value)}
             placeholder={placeholder} required={required}/>
      <datalist id={listId}>
        {options.map(o => <option key={o} value={o}/>)}
      </datalist>
      <span className="hint">{hint}</span>
    </>
  )
}
