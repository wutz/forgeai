import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import './redesign.css'
import App from './App'
import {watchTheme} from './theme'

watchTheme()

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
