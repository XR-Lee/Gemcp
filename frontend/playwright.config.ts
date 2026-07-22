import { defineConfig } from '@playwright/test'

const port = process.env.PLAYWRIGHT_PORT ?? '5187'
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  use: {
    baseURL,
    trace: 'retain-on-failure',
  },
  webServer: {
    command: `npm run dev -- --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
  },
})
