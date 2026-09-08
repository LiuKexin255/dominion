import { useState } from 'react'

// The marker string rendered here is the single-source constant asserted by
// dist_assert.sh (specs/050-vite-react-bazel/contracts/
// dist-artifact-assertions.md — behaviour requirement 4); change both together.
export function App() {
  const [count, setCount] = useState(0)

  return (
    <div data-testid="demo-marker">
      <p>dominion-vite-react-demo</p>
      <button type="button" onClick={() => setCount((c) => c + 1)}>
        increment
      </button>
      <span data-testid="demo-count">{count}</span>
    </div>
  )
}
