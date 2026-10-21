// Served by the file proxy to the dufs listing page (see fileproxy.go).
//
// WKWebView does not show prompt(), confirm() or alert(), so dufs' New folder
// and New file buttons never produced anything in the app: their prompt()
// returned nothing. This asks for the name in the page instead and sends the
// same request dufs would; the proxy adds the credentials. The login widget is
// hidden as well: the account shown there is the service account, and the
// proxy signs every request in, so logging out has no meaning here.
(() => {
  const style = document.createElement('style')
  style.textContent = '.logout-btn, .login-btn { display: none !important; }'
  document.head.appendChild(style)

  // newUrl mirrors dufs: the name may contain slashes, each part is escaped.
  const newUrl = name => {
    let base = location.href.split(/[?#]/)[0]
    if (!base.endsWith('/')) base += '/'
    return base + name.split('/').map(encodeURIComponent).join('/')
  }

  // ask shows a small modal and resolves with the entered name, or null.
  const ask = title => new Promise(resolve => {
    const overlay = document.createElement('div')
    overlay.style.cssText = 'position:fixed;inset:0;z-index:9999;display:flex;align-items:center;' +
      'justify-content:center;background:rgba(0,0,0,.25);font:14px system-ui,sans-serif'
    overlay.innerHTML = `
      <form style="background:#fff;border-radius:8px;padding:18px;width:320px;box-shadow:0 8px 24px rgba(0,0,0,.2)">
        <div style="font-weight:600;margin-bottom:10px"></div>
        <input type="text" required style="width:100%;box-sizing:border-box;padding:7px 9px;font:inherit;
          border:1px solid #ccc;border-radius:6px">
        <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:14px">
          <button type="button" data-cancel style="padding:6px 12px;font:inherit">Cancel</button>
          <button type="submit" style="padding:6px 12px;font:inherit">Create</button>
        </div>
      </form>`
    overlay.querySelector('div').textContent = title
    const form = overlay.querySelector('form')
    const input = overlay.querySelector('input')
    const done = value => {
      overlay.remove()
      resolve(value)
    }
    form.addEventListener('submit', e => {
      e.preventDefault()
      const name = input.value.trim()
      if (name) done(name)
    })
    overlay.querySelector('[data-cancel]').addEventListener('click', () => done(null))
    overlay.addEventListener('keydown', e => {
      if (e.key === 'Escape') done(null)
    })
    overlay.addEventListener('mousedown', e => {
      if (e.target === overlay) done(null)
    })
    document.body.appendChild(overlay)
    input.focus()
  })

  // notify shows an error for a few seconds; the page has no other place for it.
  const notify = message => {
    const box = document.createElement('div')
    box.setAttribute('role', 'alert')
    box.style.cssText = 'position:fixed;top:12px;left:50%;transform:translateX(-50%);z-index:9999;' +
      'background:#b42318;color:#fff;padding:8px 14px;border-radius:6px;font:13px system-ui,sans-serif'
    box.textContent = message
    document.body.appendChild(box)
    setTimeout(() => box.remove(), 5000)
  }

  const create = async folder => {
    const name = await ask(folder ? 'New folder' : 'New file')
    if (!name) return
    const url = newUrl(name)
    try {
      const res = await fetch(url, folder ? { method: 'MKCOL' } : { method: 'PUT', body: '' })
      if (!res.ok) throw new Error(await res.text() || `HTTP ${res.status}`)
      // The folder opens with its slash: the proxy adds this script only to listings.
      location.href = folder ? url + '/' : url + '?edit'
    } catch (err) {
      notify(`Cannot create ${name}: ${err.message}`)
    }
  }

  // Capture phase on window runs before dufs' own listeners, which would
  // otherwise call prompt().
  window.addEventListener('click', e => {
    if (!(e.target instanceof Element)) return
    const link = e.target.closest('a[target=_blank]')
    if (link) {
      // dufs opens files in a new window, and WKWebView does nothing for that
      // here. Open the file in this frame on dufs' viewer page instead, which
      // has a breadcrumb back to the folder.
      e.preventDefault()
      const url = new URL(link.href)
      if (!url.search) url.search = '?view'
      location.href = url.href
      return
    }
    const button = e.target.closest('.new-folder, .new-file')
    if (!button) return
    e.preventDefault()
    e.stopImmediatePropagation()
    create(button.classList.contains('new-folder'))
  }, true)
})()
