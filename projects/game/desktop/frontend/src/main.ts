import { mount } from 'svelte'
import App from './App.svelte'
import { log } from './logger'
import './style.css'

const target = document.getElementById('app')
if (!target) {
  throw new Error('app mount target not found')
}

const app = mount(App, {
  target
})

// Listen for backend log events via Wails runtime
// In production, Wails injects window.runtime with EventsOn
const runtime = window.runtime
if (runtime?.EventsOn) {
  runtime.EventsOn('game:log', (entry: unknown) => {
    const e = entry as { level?: string; source?: string; message?: string; fields?: Record<string, string> }
    log(
      e.level || 'info',
      e.source || 'backend',
      e.message || '',
      e.fields
    )
  })
}

export default app
