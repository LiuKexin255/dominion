import './style.css'
import App from './App.svelte'
import { log } from './logger'

// Mount the Svelte app
const app = new App({
  target: document.getElementById('app')!
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
