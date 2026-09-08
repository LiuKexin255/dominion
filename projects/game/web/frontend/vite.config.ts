import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://github.com/vitejs/vite-plugin-react/tree/main/packages/plugin-react
export default defineConfig({
  plugins: [react()],
})
