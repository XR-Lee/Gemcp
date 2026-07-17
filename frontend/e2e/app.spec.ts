import { expect, test, type Page, type Route } from '@playwright/test'

const build = { name: 'Gemcp', version: '0.5.0', commit: 'abc1234', built_at: '2026-07-16T00:00:00Z' }
const project = {
  id: 'b492cbe4-f198-4d87-bbf9-3f77d8a3ab0a', name: 'Point Models', slug: 'point-models', status: 'active',
  monthly_budget_milli: 100000, max_experiment_milli: 20000, max_concurrency: 2, max_runtime_seconds: 86400,
  timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
}
const repositories = [
  {
    id: '558f97ca-b648-4e7e-a329-0e03bfd58155', project_id: project.id, name: 'dynamic-point-mamba',
    ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
    deploy_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakePublicKeyForVisualTestingOnly gemcp-test',
    host_key_fingerprint: 'SHA256:trusted-test-fingerprint', last_verified_at: '2026-07-16T08:22:00Z',
  },
  {
    id: 'cd3bb6ae-fc36-47f7-8715-ce044b676515', project_id: project.id, name: 'new-baseline',
    ssh_url: 'git@github.com:research/new-baseline.git', default_branch: 'main', status: 'pending_key',
    deploy_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAnotherFakePublicKeyForVisualTestingOnly gemcp-test',
  },
]
const experiments = [
  {
    id: 'ec29dc68-9674-4611-9d8c-542f68f5e31c', project_id: project.id, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'queued', desired_state: 'running',
    commit_sha: '0123456789012345678901234567890123456789', command: 'python train.py --config configs/scanobjectnn.yaml',
    max_runtime_seconds: 14400, reserved_cost_milli: 12250, estimated_cost_milli: 0,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/ec29dc68/',
    created_at: '2026-07-16T09:30:00Z', updated_at: '2026-07-16T09:30:00Z', metrics: {},
  },
  {
    id: '11276758-f089-49e8-b706-0aa1cd0f9ac0', project_id: project.id, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'failed', desired_state: 'running',
    commit_sha: 'abcdefabcdefabcdefabcdefabcdefabcdefabcd', command: 'python evaluate.py --checkpoint latest.pt',
    max_runtime_seconds: 3600, reserved_cost_milli: 3500, estimated_cost_milli: 2180,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/11276758/', failure_code: 'PROCESS_EXIT',
    failure_reason: 'Process exited with status 1', created_at: '2026-07-15T05:20:00Z', updated_at: '2026-07-15T06:02:00Z',
    finished_at: '2026-07-15T06:02:00Z', exit_code: 1, metrics: { accuracy: 0.82 },
  },
]
const provider = {
  id: 'provider-id', name: 'AutoDL Private Cloud', base_url: 'https://private.autodl.com', backend: 'private',
  status: 'active', credential_configured: true, last_validated_at: '2026-07-17T02:00:00Z',
  created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const providerDeployment = {
  uuid: 'deployment-live-1', name: 'live-job', type: 'Job', status: 'running', replica_num: 1, parallelism_num: 1,
  starting_num: 0, running_num: 1, finished_num: 0, failed_num: 0, image_uuid: 'base-image-1',
  reuse_container: true, price_estimate_milli: 1000, created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const activeProviderContainer = {
  uuid: 'container-live-1', deployment_uuid: providerDeployment.uuid, machine_id: 'machine-safe-id', status: 'running',
  gpu_name: 'NVIDIA GeForce RTX 3090', gpu_num: 1, cpu_num: 8, memory_bytes: 34359738368,
  image_uuid: 'base-image-1', price_milli_per_hour: 1000, released: false,
  started_at: '2026-07-17T01:56:00Z', created_at: '2026-07-17T01:55:30Z',
}
const cachedProviderContainer = {
  ...activeProviderContainer, uuid: 'container-cache-1', deployment_uuid: 'deployment-old-1', status: 'in_cache', released: true,
  started_at: '2026-07-16T10:00:00Z', stopped_at: '2026-07-16T10:30:00Z', created_at: '2026-07-16T09:59:00Z',
}
const providerResources = {
  generated_at: '2026-07-17T02:00:00Z', provider,
  gpu_stock: [{ name: 'NVIDIA GeForce RTX 3090', idle: 2, total: 9 }],
  private_images: [],
  system_images: [
    { uuid: 'base-image-1', name: 'torch:cuda11.8-cudnn8-devel-ubuntu22.04-py310-torch2.1.2', cuda_version: '11.8', chip_corp: 'nvidia', cpu_arch: 'x86', source: 'system' },
    { uuid: 'base-image-2', name: 'miniconda:cuda12.2-cudnn8-devel-ubuntu22.04-py310', cuda_version: '12.2', chip_corp: 'nvidia', cpu_arch: 'x86', source: 'system' },
  ],
  deployments: [providerDeployment], active_containers: [activeProviderContainer], cached_containers: [cachedProviderContainer],
}

async function fulfill(route: Route, data: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(status >= 400 ? data : { data }) })
}

async function mockSetup(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: false })
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function mockLogin(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: true })
    if (path === '/api/v1/auth/me') return fulfill(route, { error: { code: 'UNAUTHENTICATED', message: 'authentication required' } }, 401)
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function mockConsole(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: true })
    if (path === '/api/v1/auth/me') return fulfill(route, { user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@example.com', role: 'owner' })
    if (path === '/api/v1/provider' && route.request().method() === 'GET') return fulfill(route, provider)
    if (path === '/api/v1/provider' && route.request().method() === 'PUT') return fulfill(route, { provider, resources: providerResources })
    if (path === '/api/v1/provider/query') return fulfill(route, providerResources)
    if (path === `/api/v1/provider/deployments/${providerDeployment.uuid}`) return fulfill(route, {
      generated_at: '2026-07-17T02:00:01Z', deployment: providerDeployment,
      active_containers: [activeProviderContainer], released_containers: [],
      events: [
        { container_uuid: activeProviderContainer.uuid, status: 'running', created_at: '2026-07-17T01:56:00Z' },
        { container_uuid: activeProviderContainer.uuid, status: 'starting', created_at: '2026-07-17T01:55:30Z' },
      ],
    })
    if (path === '/api/v1/projects') return fulfill(route, [project])
    if (path === '/api/v1/repositories') return fulfill(route, repositories)
    if (path === '/api/v1/experiments') return fulfill(route, experiments)
    if (path === `/api/v1/projects/${project.id}/cost`) return fulfill(route, {
      period: '2026-07', monthly_budget_milli: 100000, reserved_milli: 15750, charged_milli: 2180,
      adjustments_milli: 0, committed_milli: 17930, available_milli: 82070,
    })
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function expectNoPageOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }))
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.width + 1)
}

test('first-run setup fits desktop and mobile', async ({ page }) => {
  await mockSetup(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Create the Owner' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-mobile.png', fullPage: true })

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByLabel('Bootstrap token').fill('gmb_abcdefghijklmnopqrstuvwxyz123456')
  await page.getByLabel('Organization name').fill('Research Lab')
  await page.getByLabel('Owner email').fill('owner@example.com')
  await page.getByLabel('Owner password').fill('correct horse battery staple')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByLabel('AutoDL service')).toHaveValue('private')
  await expect(page.getByLabel('API base URL')).toHaveValue('https://private.autodl.com')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-provider-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-provider-mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByLabel('AutoDL service').selectOption('public')
  await expect(page.getByLabel('API base URL')).toHaveValue('https://api.autodl.com')
  await page.getByLabel('AutoDL service').selectOption('private')
  await page.getByLabel('AutoDL API token').fill('test-provider-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('heading', { name: 'Configure the first project' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-project-desktop.png', fullPage: true })
})

test('Owner login fits desktop and mobile', async ({ page }) => {
  await mockLogin(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-login-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-login-mobile.png', fullPage: true })
})

test('operations console and dialogs fit desktop', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  await expect(page.getByText('CNY 82.07')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-console-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Project', exact: true }).click()
  await expect(page.getByRole('cell', { name: 'dynamic-point-mamba', exact: true })).toBeVisible()
  await page.getByTitle('View Deploy public key').first().click()
  await expect(page.getByRole('heading', { name: 'Deploy public key' })).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-repository-dialog.png', fullPage: true })
  await page.getByTitle('Close').click()

  await page.getByRole('button', { name: 'Experiments', exact: true }).click()
  await page.getByText('ec29dc68').click()
  await expect(page.getByRole('dialog', { name: 'Experiment details' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-experiment-detail.png', fullPage: true })
})

test('live Provider resources and details fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Private Cloud resources' })).toBeVisible()
  await expect(page.getByText('2 / 9')).toBeVisible()
  await expect(page.getByText('NVIDIA GeForce RTX 3090')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-provider-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'images', exact: true }).click()
  await expect(page.getByText('torch:cuda11.8-cudnn8-devel-ubuntu22.04-py310-torch2.1.2')).toBeVisible()
  await page.getByRole('button', { name: 'deployments', exact: true }).click()
  await page.getByText('live-job', { exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Provider deployment details' })).toBeVisible()
  await expect(page.getByText('container-live-1').first()).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-provider-deployment.png', fullPage: true })
  await page.getByTitle('Close details').click()

  await page.getByRole('button', { name: 'Rotate token' }).click()
  await expect(page.getByRole('dialog', { name: 'Rotate Provider token' })).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-provider-token-dialog.png', fullPage: true })
  await page.getByLabel('Developer Token').fill('preview-token-that-must-be-cleared-on-close')
  await page.getByTitle('Close').click()
  await page.getByRole('button', { name: 'Rotate token' }).click()
  await expect(page.getByLabel('Developer Token')).toHaveValue('')
  await page.getByTitle('Close').click()

  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'inventory', exact: true }).click()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-provider-mobile.png', fullPage: true })
})

test('operations console uses bottom navigation on mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible()
  await expectNoPageOverflow(page)
  const navigation = page.getByRole('navigation', { name: 'Primary navigation' })
  await expect(navigation).toBeVisible()
  const box = await navigation.boundingBox()
  expect(box?.y ?? 0).toBeGreaterThan(780)
  await page.screenshot({ path: '/tmp/gemcp-console-mobile.png', fullPage: true })
})
