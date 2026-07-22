import { readFile, unlink } from 'node:fs/promises'
import { expect, test, type Page, type Route } from '@playwright/test'

const build = { name: 'Gemcp', version: '0.7.0', commit: 'abc1234', built_at: '2026-07-16T00:00:00Z' }
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
const agentTokens = [
  {
    id: 'agent-token-active-1', project_id: project.id, label: 'default-agent', prefix: 'gmc_abcd123',
    scopes: ['read', 'submit', 'cancel'], status: 'active', expires_at: '2026-10-15T02:00:00Z',
    last_used_at: '2026-07-17T01:58:00Z', created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-17T01:58:00Z',
  },
  {
    id: 'agent-token-revoked-1', project_id: project.id, label: 'retired-agent', prefix: 'gmc_old1234',
    scopes: ['read'], status: 'revoked', created_at: '2026-06-01T00:00:00Z', updated_at: '2026-07-01T00:00:00Z',
  },
]
const agentEnrollments = [
  {
    id: 'agent-enrollment-completed-1', project_id: project.id, label: 'pi-research-agent',
    scopes: ['read', 'submit', 'cancel'], status: 'completed', expires_at: '2026-07-17T01:00:00Z',
    token_expires_in_days: 30, claimed_at: '2026-07-17T00:31:00Z', completed_at: '2026-07-17T00:32:00Z',
    agent_token_id: agentTokens[0].id, agent_token_prefix: agentTokens[0].prefix,
    created_at: '2026-07-17T00:30:00Z', updated_at: '2026-07-17T00:32:00Z',
  },
]
const issuedEnrollment = {
  id: 'agent-enrollment-issued-1', project_id: project.id, label: 'pi-integration-agent',
  scopes: ['read', 'submit', 'cancel'], status: 'pending', expires_at: '2026-07-17T02:15:00Z',
  token_expires_in_days: 30, created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const issuedSetupURL = 'https://gemcp.example.com/agent/setup#code=gme_setup1_test-capability'
const issuedAgentToken = 'gmc_new1234_test-secret'
const issuedAgent = {
  id: 'agent-token-issued-1', project_id: project.id, label: 'integration-agent', prefix: 'gmc_new1234',
  scopes: ['read', 'submit', 'cancel'], status: 'active', expires_at: '2026-08-16T02:00:00Z',
  created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const mcpConfig = {
  mcpServers: {
    'gemcp-point-models': {
      type: 'http', url: 'https://gemcp.example.com/mcp',
      headers: { Authorization: `Bearer ${issuedAgentToken}` },
    },
  },
}
const agentTokenList = {
  tokens: agentTokens, enrollments: agentEnrollments, mcp_url: 'https://gemcp.example.com/mcp', config_file_name: 'gemcp-point-models-mcp.json',
  config_template: {
    mcpServers: {
      'gemcp-point-models': {
        type: 'http', url: 'https://gemcp.example.com/mcp',
        headers: { Authorization: 'Bearer ${GEMCP_AGENT_TOKEN}' },
      },
    },
  },
}
const attemptHistory = [{
  id: 'attempt-live-1', number: 1, state: 'running', provider_resource_id: 'deployment-live-1',
  estimated_cost_milli: 0, log_tail: 'epoch 3 loss=0.42\n', metrics: { loss: 0.42 },
  started_at: '2026-07-17T01:56:00Z', created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}]
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
const managedProviderResource = {
  id: 'managed-resource-1', experiment_id: experiments[0].id, attempt_id: 'attempt-live-1',
  provider_id: providerDeployment.uuid, name: 'live-job', state: 'active', provider_status: 'running',
  hard_deadline_at: '2026-07-17T06:00:00Z', last_seen_at: '2026-07-17T02:00:00Z',
  created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
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
const runtimeStatus = {
  scheduler_enabled: true, self_hosted_enabled: false, global_concurrency: 2, public_url_configured: true,
  scheduler_healthy: true, watchdog_healthy: true, notification_worker_healthy: true,
  scheduler_heartbeat: { role: 'scheduler', instance_id: 'controlplane-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  watchdog_heartbeat: { role: 'watchdog', instance_id: 'watchdog-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  notification_heartbeat: { role: 'notification', instance_id: 'notification-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  generated_at: '2026-07-17T02:00:01Z',
}
const selfHostedNode = {
  id: 'node-11111111-2222-4333-8444-555555555555', label: 'lab-gpu-01', token_prefix: 'gmn_test',
  status: 'active', observed_state: 'online', installation_id: 'installation-id', machine_fingerprint: 'a'.repeat(64),
  hostname: 'gpu-workstation', operating_system: 'linux', architecture: 'amd64', agent_version: '0.9.0', protocol_version: '1',
  capabilities: { cpu_count: 16, memory_bytes: 68719476736, gpus: [{ uuid: 'GPU-test', name: 'NVIDIA GeForce RTX 3090', memory_bytes: 25769803776 }] },
  storage: { root: '/var/lib/gemcp-node/storage', total_bytes: 1099511627776, available_bytes: 824633720832, managed_bytes: 21474836480 },
  project_ids: [project.id], last_seen_at: '2026-07-17T02:00:00Z', approved_at: '2026-07-17T01:00:00Z',
  created_at: '2026-07-17T00:30:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const selfHostedAssignment = {
  id: 'assignment-id', node_id: selfHostedNode.id, node_label: selfHostedNode.label, project_id: project.id,
  experiment_id: experiments[0].id, attempt_id: 'attempt-self-hosted-1', attempt_number: 1, state: 'running',
  output_ref: `experiments/${experiments[0].id}/attempts/attempt-self-hosted-1/outputs`,
  started_at: '2026-07-17T01:56:00Z', last_heartbeat_at: '2026-07-17T02:00:00Z', metrics: { loss: 0.42 },
  created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const selfHostedRuntimes = {
  environments: [{ id: 'environment-self-hosted', name: 'local-3090', image: `registry.example/train@sha256:${'b'.repeat(64)}`, is_default: true }],
  resource_profiles: [{ id: 'profile-self-hosted', name: 'local-3090', gpu_names: ['NVIDIA GeForce RTX 3090'], cpu_limit: 8, memory_gb: 32, is_default: true }],
}
const notificationSetting = {
  configured: true, enabled: true, host: 'smtp.example.com', port: 587, tls_mode: 'starttls',
  username: 'mailer@example.com', password_configured: true, from_address: 'mailer@example.com',
  recipients: ['owner@example.com'], status: 'ready', last_tested_at: '2026-07-17T01:30:00Z', updated_at: '2026-07-17T01:30:00Z',
}
const notificationDeliveries = [
  { id: 'notification-1', kind: 'watchdog_stop', severity: 'critical', subject: '[Gemcp] Watchdog enforced Provider shutdown', state: 'sent', attempts: 1, next_attempt_at: '2026-07-17T01:00:00Z', sent_at: '2026-07-17T01:00:02Z', created_at: '2026-07-17T01:00:00Z' },
  { id: 'notification-2', kind: 'timeout_extended', severity: 'warning', subject: '[Gemcp] Experiment runtime extended', state: 'pending', attempts: 0, next_attempt_at: '2026-07-17T02:01:00Z', created_at: '2026-07-17T02:00:00Z' },
]
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

async function mockConsole(page: Page, counters?: { providerQueries: number; selfHosted?: boolean }) {
  await page.route('**/docs/*.md', async (route) => {
    const path = new URL(route.request().url()).pathname
    const body = path.endsWith('/agent-mcp.md')
      ? '# Gemcp MCP Agent Operating Guide\n\n## Non-negotiable rules\nCall get_project_options and get_project_cost before submission.\n'
      : '# Gemcp MCP Owner Guide\n\n## Owner onboarding checklist\nConfigure the MCP client without exposing its Agent Token.\n'
    await route.fulfill({ status: 200, contentType: 'text/markdown', headers: { 'Access-Control-Allow-Origin': '*' }, body })
  })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: true })
    if (path === '/api/v1/auth/me') return fulfill(route, { user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@example.com', role: 'owner' })
    if (path === '/api/v1/runtime/status') return fulfill(route, { ...runtimeStatus, self_hosted_enabled: Boolean(counters?.selfHosted) })
    if (path === '/api/v1/nodes' && counters?.selfHosted) return fulfill(route, {
      nodes: [selfHostedNode], enrollments: [], assignments: [selfHostedAssignment],
    })
    if (path === `/api/v1/projects/${project.id}/self-hosted-runtimes` && counters?.selfHosted) return fulfill(route, selfHostedRuntimes)
    if (path === '/api/v1/provider' && route.request().method() === 'GET') return fulfill(route, provider)
    if (path === '/api/v1/provider' && route.request().method() === 'PUT') return fulfill(route, { provider, resources: providerResources })
    if (path === '/api/v1/provider/query') {
      if (counters) counters.providerQueries += 1
      return fulfill(route, providerResources)
    }
    if (path === '/api/v1/provider/managed-resources') return fulfill(route, [managedProviderResource])
    if (path === `/api/v1/provider/deployments/${providerDeployment.uuid}/stop`) return fulfill(route, { ...managedProviderResource, stop_requested_at: '2026-07-17T02:01:00Z', stop_reason: 'owner_stop' }, 202)
    if (path === '/api/v1/provider/emergency-stop') {
      expect(route.request().postDataJSON()).toEqual({ confirmation: 'STOP' })
      return fulfill(route, { requested: 1, at: '2026-07-17T02:01:00Z' }, 202)
    }
    if (path === '/api/v1/notifications/settings') return fulfill(route, notificationSetting)
    if (path === '/api/v1/notifications') return fulfill(route, notificationDeliveries)
    if (path === '/api/v1/notifications/test') return fulfill(route, { id: 'notification-test', kind: 'smtp_test', severity: 'info', subject: 'Gemcp SMTP test', state: 'pending', attempts: 0, next_attempt_at: '2026-07-17T02:02:00Z', created_at: '2026-07-17T02:02:00Z' }, 202)
    if (path === `/api/v1/provider/deployments/${providerDeployment.uuid}`) return fulfill(route, {
      generated_at: '2026-07-17T02:00:01Z', deployment: providerDeployment,
      active_containers: [activeProviderContainer], released_containers: [],
      events: [
        { container_uuid: activeProviderContainer.uuid, status: 'running', created_at: '2026-07-17T01:56:00Z' },
        { container_uuid: activeProviderContainer.uuid, status: 'starting', created_at: '2026-07-17T01:55:30Z' },
      ],
    })
    if (path === '/api/v1/projects') return fulfill(route, [project])
    if (path === `/api/v1/projects/${project.id}/agent-tokens` && route.request().method() === 'GET') return fulfill(route, agentTokenList)
    if (path === `/api/v1/projects/${project.id}/agent-enrollments` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({
        label: 'pi-integration-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30,
        never_expires: false, setup_expires_in_minutes: 15,
      })
      return fulfill(route, {
        enrollment: issuedEnrollment, setup_url: issuedSetupURL,
        installer_url: 'https://gemcp.example.com/agent/setup/install.mjs',
      }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/agent-enrollments/${issuedEnrollment.id}` && route.request().method() === 'DELETE') {
      return fulfill(route, { ...issuedEnrollment, status: 'revoked', updated_at: '2026-07-17T02:01:00Z' })
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({
        label: 'integration-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30, never_expires: false,
      })
      return fulfill(route, {
        token: issuedAgent, agent_token: issuedAgentToken, mcp_url: 'https://gemcp.example.com/mcp',
        mcp_config: mcpConfig, config_file_name: 'gemcp-point-models-mcp.json',
      }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens/${agentTokens[0].id}` && route.request().method() === 'DELETE') {
      return fulfill(route, { ...agentTokens[0], status: 'revoked', updated_at: '2026-07-17T02:01:00Z' })
    }
    if (path === '/api/v1/repositories') return fulfill(route, repositories)
    if (path === '/api/v1/experiments') return fulfill(route, experiments)
    if (path === `/api/v1/experiments/${experiments[0].id}/attempts`) return fulfill(route, attemptHistory)
    if (path === `/api/v1/experiments/${experiments[0].id}`) return fulfill(route, experiments[0])
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
  await expect(page.getByText('epoch 3 loss=0.42')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-experiment-detail.png', fullPage: true })
})

test('Self-hosted nodes, Assignments and runtime configuration fit desktop and mobile', async ({ page }) => {
  await mockConsole(page, { providerQueries: 0, selfHosted: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Nodes', exact: true }).click()
  await expect(page.getByText('gpu-workstation', { exact: false })).toBeVisible()
  await expect(page.getByText('local-3090', { exact: true })).toBeVisible()
  await expect(page.getByText('ec29dc68-967', { exact: true })).toBeVisible()
  await expect(page.getByText('gmn_test', { exact: true })).toHaveCount(0)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-nodes-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Add runtime', exact: true }).click()
  const runtimeDialog = page.locator('.runtime-dialog')
  await expect(runtimeDialog.getByRole('heading', { name: 'Add Self-hosted runtime' })).toBeVisible()
  await expect(runtimeDialog.getByLabel('Accepted GPU models')).toHaveValue('NVIDIA GeForce RTX 3090')
  await runtimeDialog.getByLabel('Runtime name').fill('second-node-runtime')
  await runtimeDialog.getByLabel('OCI image pinned by digest').fill(`registry.example/second@sha256:${'c'.repeat(64)}`)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-runtime-dialog-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-runtime-dialog-mobile.png', fullPage: true })
  await runtimeDialog.getByRole('button', { name: 'Close' }).click()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-nodes-mobile.png', fullPage: true })
})

test('Pi setup links, MCP guidance and advanced token controls fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Agent access', level: 1 })).toBeVisible()
  await expect(page.getByText('default-agent', { exact: true })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'default-agent' }).getByText('gmc_abcd123', { exact: true })).toBeVisible()
  await expect(page.getByText('pi-research-agent', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agents-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Pi setup link', exact: true }).click()
  const setupDialog = page.getByRole('dialog', { name: 'Create Pi setup link' })
  await expect(setupDialog).toBeVisible()
  await setupDialog.getByLabel('Agent label').fill('pi-integration-agent')
  await setupDialog.getByLabel('Credential expiration').selectOption('30')
  await setupDialog.getByLabel('Link validity').selectOption('15')
  await expect(setupDialog.getByLabel('Read')).toBeChecked()
  await expect(setupDialog.getByLabel('Submit')).toBeChecked()
  await expect(setupDialog.getByLabel('Cancel')).toBeChecked()
  await setupDialog.getByRole('button', { name: 'Create setup link' }).click()

  const setupReveal = page.getByRole('dialog', { name: 'One-time Pi setup link' })
  await expect(setupReveal).toBeVisible()
  await expect(setupReveal.getByText(issuedSetupURL, { exact: true })).toBeVisible()
  await expect(setupReveal.getByText('Give the Agent only this link', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-setup-link.png', fullPage: true })
  await setupReveal.getByTitle('Close Pi setup link').click()
  await expect(page.getByText(issuedSetupURL, { exact: true })).toHaveCount(0)
  const setupRow = page.getByRole('row').filter({ hasText: 'pi-integration-agent' })
  await expect(setupRow).toBeVisible()
  await setupRow.getByTitle('Revoke Pi setup link').click()
  const setupRevokeDialog = page.getByRole('alertdialog', { name: 'Revoke Pi setup link' })
  await expect(setupRevokeDialog).toBeVisible()
  await setupRevokeDialog.getByRole('button', { name: 'Revoke link' }).click()
  await expect(setupRow.getByText('revoked', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Guide', exact: true }).click()
  const guideDialog = page.getByRole('dialog', { name: 'Gemcp MCP onboarding guide' })
  await expect(guideDialog).toBeVisible()
  await expect(guideDialog.getByText('get_usage_guide', { exact: true })).toBeVisible()
  await expect(guideDialog.getByText('gemcp://docs/agent-guide', { exact: true })).toBeVisible()
  await expect(guideDialog.getByRole('link', { name: 'Download Agent handoff' })).toHaveAttribute('href', 'https://gemcp.example.com/docs/agent-mcp.md')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-guide-desktop.png', fullPage: true })
  await guideDialog.getByTitle('Close MCP guide').click()

  await page.getByRole('button', { name: 'Token', exact: true }).click()
  const issueDialog = page.getByRole('dialog', { name: 'Generate Agent token' })
  await expect(issueDialog).toBeVisible()
  await issueDialog.getByLabel('Label').fill('integration-agent')
  await issueDialog.getByLabel('Expiration').selectOption('30')
  await expect(issueDialog.getByLabel('Read')).toBeChecked()
  await expect(issueDialog.getByLabel('Submit')).toBeChecked()
  await expect(issueDialog.getByLabel('Cancel')).toBeChecked()
  await issueDialog.getByRole('button', { name: 'Generate token' }).click()

  const revealDialog = page.getByRole('dialog', { name: 'Agent token and MCP configuration' })
  await expect(revealDialog).toBeVisible()
  await expect(revealDialog.getByText(issuedAgentToken, { exact: true })).toBeVisible()
  const downloadPromise = page.waitForEvent('download')
  await revealDialog.getByRole('button', { name: 'Download JSON' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('gemcp-point-models-mcp.json')
  const downloadPath = '/tmp/gemcp-exported-mcp.json'
  await download.saveAs(downloadPath)
  const exported = JSON.parse(await readFile(downloadPath, 'utf8'))
  expect(exported).toEqual(mcpConfig)
  await unlink(downloadPath)
  await page.screenshot({ path: '/tmp/gemcp-agent-token-reveal.png', fullPage: true })
  await revealDialog.getByTitle('Close Agent token').click()
  await expect(page.getByText(issuedAgentToken, { exact: true })).toHaveCount(0)

  const defaultRow = page.getByRole('row').filter({ hasText: 'default-agent' })
  await defaultRow.getByTitle('Revoke Agent token').click()
  const revokeDialog = page.getByRole('alertdialog', { name: 'Revoke Agent token' })
  await expect(revokeDialog).toBeVisible()
  await revokeDialog.getByRole('button', { name: 'Revoke token' }).click()
  await expect(defaultRow.getByText('revoked', { exact: true })).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.getByRole('button', { name: 'Guide', exact: true }).click()
  await expect(guideDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-guide-mobile.png', fullPage: true })
  await guideDialog.getByTitle('Close MCP guide').click()
  await page.getByRole('button', { name: 'Pi setup link', exact: true }).click()
  await expect(setupDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-setup-form-mobile.png', fullPage: true })
  await setupDialog.getByTitle('Close setup link form').click()
  await page.screenshot({ path: '/tmp/gemcp-agents-mobile.png', fullPage: true })
})

test('live Provider resources and details fit desktop and mobile', async ({ page }) => {
  const counters = { providerQueries: 0 }
  await mockConsole(page, counters)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Private Cloud resources' })).toBeVisible()
  await expect(page.getByText('2 / 9')).toBeVisible()
  await expect(page.getByText('NVIDIA GeForce RTX 3090')).toBeVisible()
  await expect.poll(() => counters.providerQueries).toBe(1)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-provider-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Overview', exact: true }).click()
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByText('NVIDIA GeForce RTX 3090')).toBeVisible()
  expect(counters.providerQueries).toBe(1)

  await page.getByRole('button', { name: 'images', exact: true }).click()
  await expect(page.getByText('torch:cuda11.8-cudnn8-devel-ubuntu22.04-py310-torch2.1.2')).toBeVisible()
  await page.getByRole('button', { name: 'deployments', exact: true }).click()
  await page.getByText('live-job', { exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Provider deployment details' })).toBeVisible()
  await expect(page.getByText('container-live-1').first()).toBeVisible()
  await expect(page.getByText('Managed').last()).toBeVisible()
  await page.getByRole('button', { name: 'Stop deployment' }).click()
  await expect(page.getByRole('button', { name: 'Stop requested' })).toBeDisabled()
  await page.screenshot({ path: '/tmp/gemcp-provider-deployment.png', fullPage: true })
  await page.getByTitle('Close details').click()

  await page.getByRole('button', { name: 'Emergency stop' }).click()
  await expect(page.getByRole('alertdialog', { name: 'Emergency stop all managed resources' })).toBeVisible()
  await page.getByLabel('Type STOP to confirm').fill('STOP')
  await page.screenshot({ path: '/tmp/gemcp-provider-emergency-stop.png', fullPage: true })
  await page.getByRole('button', { name: 'Stop all managed resources' }).click()

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

test('durable notification outbox and SMTP settings fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Alerts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible()
  await expect(page.getByText('[Gemcp] Watchdog enforced Provider shutdown')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-notifications-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Send test' }).click()
  await expect(page.getByText('Gemcp SMTP test')).toBeVisible()
  await page.getByRole('button', { name: 'SMTP settings' }).click()
  await expect(page.getByRole('dialog', { name: 'SMTP notification settings' })).toBeVisible()
  await expect(page.locator('input[type="password"]')).toHaveValue('')
  await page.screenshot({ path: '/tmp/gemcp-smtp-settings.png', fullPage: true })
  await page.getByTitle('Close SMTP settings').click()

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-notifications-mobile.png', fullPage: true })
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
  await navigation.getByRole('button', { name: 'Experiments', exact: true }).click()
  await page.getByText('ec29dc68').click()
  await expect(page.getByRole('dialog', { name: 'Experiment details' })).toBeVisible()
  await expect(page.getByText('epoch 3 loss=0.42')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-experiment-detail-mobile.png', fullPage: true })
})
