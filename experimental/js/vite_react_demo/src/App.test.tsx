// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { App } from './App'

// vitest does not expose a global afterEach here (no `globals: true`), so RTL's
// auto-cleanup never registers — clean up explicitly to keep test DOM isolated.
afterEach(cleanup)

describe('App', () => {
  it('renders the demo marker', () => {
    render(<App />)
    const marker = screen.getByTestId('demo-marker')
    expect(marker.textContent).toContain('dominion-vite-react-demo')
  })

  it('increments the counter when the button is clicked', () => {
    render(<App />)
    const count = screen.getByTestId('demo-count')
    expect(count.textContent).toBe('0')

    fireEvent.click(screen.getByRole('button'))
    expect(count.textContent).toBe('1')

    fireEvent.click(screen.getByRole('button'))
    expect(count.textContent).toBe('2')
  })
})
