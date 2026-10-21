import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import './redesign.css'
import App from './App'
import {locale} from './i18n'
import {watchTheme} from './theme'

document.documentElement.lang = locale
watchTheme()

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
