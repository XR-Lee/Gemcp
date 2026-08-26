#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const trustedOrigin = '{{GEMCP_PUBLIC_URL}}'
const setupUserAgent = 'Gemcp-Pi-Setup/1'
const expectedDirectTools = [
  'get_usage_guide',
  'list_repository_registrations',
  'register_repository',
  'verify_repository',
  'list_workspace_datasets',
  'register_workspace_dataset',
  'remove_workspace_dataset',
  'list_dataset_bindings',
  'register_dataset_binding',
  'remove_dataset_binding',
  'get_research_workspace',
  'update_research_workspace',
  'get_next_actions',
  'close_run',
  'report_agent_activity',
  'prepare_experiment',
  'submit_prepared_experiment',
  'get_project_options',
  'get_project_cost',
  'submit_experiment',
  'get_experiment',
  'list_experiments',
  'cancel_experiment',
  'list_artifacts',
  'register_ssh_cloud_node',
  'rotate_ssh_cloud_node_credential',
]
const allowedScopes = ['read', 'submit', 'cancel', 'configure', 'operate_nodes']

function isExpectedScopes(scopes) {
  if (!Array.isArray(scopes) || scopes.length === 0 || scopes[0] !== 'read' || scopes.length !== new Set(scopes).size) return false
  const canonical = allowedScopes.filter(scope => scopes.includes(scope))
  return canonical.length === scopes.length && scopes.every((scope, index) => scope === canonical[index])
}

function isExpectedPiConfig(definition, token = definition?.bearerToken) {
  const directTools = definition?.directTools
  return token?.startsWith('gmc_') && definition?.auth === 'bearer' && definition?.bearerToken === token &&
    definition?.lifecycle === 'lazy' && definition?.exposeResources === true && Array.isArray(directTools) &&
    directTools.length === expectedDirectTools.length && expectedDirectTools.every(tool => directTools.includes(tool))
}

function resolveAgentDir() {
  const configured = process.env.PI_CODING_AGENT_DIR?.trim()
  if (!configured) return path.join(os.homedir(), '.pi', 'agent')
  if (configured === '~') return os.homedir()
  if (configured.startsWith('~/')) return path.join(os.homedir(), configured.slice(2))
  return path.resolve(configured)
}

async function atomicWrite(filePath, content, mode) {
  const temporary = `${filePath}.${process.pid}.${crypto.randomBytes(6).toString('hex')}.tmp`
  await fs.writeFile(temporary, content, { encoding: 'utf8', mode, flag: 'wx' })
  try {
    await fs.chmod(temporary, mode)
    await fs.rename(temporary, filePath)
    await fs.chmod(filePath, mode)
  } catch (error) {
    await fs.rm(temporary, { force: true }).catch(() => {})
    throw error
  }
}

async function readJSON(filePath, fallback) {
  try {
    return JSON.parse(await fs.readFile(filePath, 'utf8'))
  } catch (error) {
    if (error?.code === 'ENOENT') return fallback
    throw new Error(`Cannot parse ${filePath}: ${error instanceof Error ? error.message : String(error)}`)
  }
}

async function acquireLock(lockPath, configPath) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      const lock = await fs.open(lockPath, 'wx', 0o600)
      await lock.writeFile(`${JSON.stringify({ pid: process.pid, created_at: new Date().toISOString() })}\n`, 'utf8')
      return lock
    } catch (error) {
      if (error?.code !== 'EEXIST') throw error
      let stale = false
      try {
        const record = JSON.parse(await fs.readFile(lockPath, 'utf8'))
        const ownerPID = Number(record?.pid ?? record)
        if (!Number.isSafeInteger(ownerPID) || ownerPID < 1) throw new Error('invalid lock owner')
        try {
          process.kill(ownerPID, 0)
        } catch (processError) {
          if (processError?.code === 'ESRCH') stale = true
          else if (processError?.code !== 'EPERM') throw processError
        }
      } catch (lockError) {
        if (lockError?.code === 'ENOENT') continue
        const stat = await fs.stat(lockPath).catch(() => null)
        stale = Boolean(stat && Date.now() - stat.mtimeMs > 10 * 60 * 1000)
      }
      if (!stale) throw new Error(`Another Gemcp setup is updating ${configPath}`)
      const stalePath = `${lockPath}.stale.${process.pid}.${crypto.randomBytes(4).toString('hex')}`
      try {
        await fs.rename(lockPath, stalePath)
        await fs.rm(stalePath, { force: true })
      } catch (renameError) {
        if (renameError?.code !== 'ENOENT') throw renameError
      }
    }
  }
  throw new Error(`Could not acquire the Gemcp setup lock for ${configPath}`)
}

async function responseData(response, operation) {
  let payload
  try {
    payload = await response.json()
  } catch {
    throw new Error(`${operation} returned HTTP ${response.status} without JSON`)
  }
  if (!response.ok) throw new Error(payload?.error?.message || `${operation} failed with HTTP ${response.status}`)
  if (!payload?.data) throw new Error(`${operation} returned no data`)
  return payload.data
}

function printSuccess({ serverName, configPath, cliPath, verification, repeated = false }) {
  process.stdout.write([
    'GEMCP_PI_SETUP_OK',
    `server=${serverName}`,
    `config=${configPath}`,
    `tools=${verification.toolCount}`,
    `checks=${verification.checks.join(',')}`,
    `already_completed=${repeated}`,
    'pi_reload_required=true',
    `current_session_cli=node ${JSON.stringify(cliPath)} ${JSON.stringify(serverName)} TOOL ARGS_JSON`,
    'next=Run /reload once for native gemcp_* tools. Until then use current_session_cli through bash.',
    '',
  ].join('\n'))
}

async function main() {
  const rawSetupLink = process.argv[2] || process.env.GEMCP_SETUP_LINK
  if (!rawSetupLink) throw new Error('Pass the complete Gemcp setup link as the first argument')
  const setupLink = new URL(rawSetupLink)
  if (setupLink.origin !== trustedOrigin || setupLink.pathname !== '/agent/setup' || setupLink.search || setupLink.username || setupLink.password) {
    throw new Error(`Refusing setup link outside ${trustedOrigin}/agent/setup`)
  }
  const fragment = new URLSearchParams(setupLink.hash.slice(1))
  const fragmentEntries = [...fragment.entries()]
  const code = fragmentEntries.length === 1 && fragmentEntries[0][0] === 'code' ? fragmentEntries[0][1] : ''
  if (!code.startsWith('gme_') || code.length < 32 || code.length > 160) {
    throw new Error('Setup link has no valid fragment code')
  }
  setupLink.hash = ''
  setupLink.search = ''

  const agentDir = resolveAgentDir()
  const configPath = path.join(agentDir, 'mcp.json')
  const adapterPackagePath = path.join(agentDir, 'npm', 'node_modules', 'pi-mcp-adapter', 'package.json')
  const runtimeDir = path.join(agentDir, 'gemcp')
  const cliPath = path.join(runtimeDir, 'gemcp-tool.mjs')
  const codeHash = crypto.createHash('sha256').update(code).digest('hex')
  const receiptPath = path.join(runtimeDir, `setup-${codeHash}.json`)
  const lockPath = `${configPath}.gemcp-setup.lock`

  await fs.mkdir(agentDir, { recursive: true })
  await fs.mkdir(runtimeDir, { recursive: true, mode: 0o700 })
  await fs.chmod(runtimeDir, 0o700)

  const existingReceipt = await readJSON(receiptPath, null)

  let adapterPackage
  try {
    adapterPackage = JSON.parse(await fs.readFile(adapterPackagePath, 'utf8'))
  } catch {
    throw new Error('pi-mcp-adapter is not installed. Install npm:pi-mcp-adapter, restart Pi, then reuse this link.')
  }

  const lock = await acquireLock(lockPath, configPath)
  try {
    const config = await readJSON(configPath, { mcpServers: {} })
    if (!config || typeof config !== 'object' || Array.isArray(config)) throw new Error(`${configPath} must contain a JSON object`)
    if (config.mcpServers === undefined) config.mcpServers = {}
    if (!config.mcpServers || typeof config.mcpServers !== 'object' || Array.isArray(config.mcpServers)) {
      throw new Error(`${configPath} mcpServers must be an object`)
    }

    const cliResponse = await fetch(`${trustedOrigin}/agent/setup/gemcp-tool.mjs`, {
      headers: { 'User-Agent': setupUserAgent }, redirect: 'error', signal: AbortSignal.timeout(30_000),
    })
    if (!cliResponse.ok) throw new Error(`Gemcp tool helper download failed with HTTP ${cliResponse.status}`)
    const cliSource = await cliResponse.text()
    if (!cliSource.includes('GEMCP_TOOL_HELPER_V1')) throw new Error('Gemcp tool helper failed integrity marker validation')
    await atomicWrite(cliPath, cliSource, 0o700)

    if (existingReceipt?.status === 'completed' && existingReceipt?.setup_origin === trustedOrigin && existingReceipt?.server_name) {
      const helper = await import(`${pathToFileURL(cliPath).href}?check=${Date.now()}`)
      const verification = await helper.verifyConfiguredServer(configPath, existingReceipt.server_name)
      printSuccess({ serverName: existingReceipt.server_name, configPath, cliPath, verification, repeated: true })
      return
    }

    let claim
    const resumableServer = existingReceipt?.status === 'verified' && existingReceipt?.setup_origin === trustedOrigin
      ? config.mcpServers[existingReceipt.server_name]
      : null
    if (isExpectedPiConfig(resumableServer) && isExpectedScopes(existingReceipt?.scopes) && resumableServer.url === existingReceipt.mcp_url) {
      claim = {
        enrollment_id: existingReceipt.enrollment_id,
        project_id: existingReceipt.project_id,
        project_name: existingReceipt.project_name,
        server_name: existingReceipt.server_name,
        scopes: existingReceipt.scopes,
        agent_token: resumableServer.bearerToken,
        pi_config: resumableServer,
      }
    } else {
      const claimResponse = await fetch(`${trustedOrigin}/api/v1/agent-enrollments/claim`, {
        method: 'POST', redirect: 'error', signal: AbortSignal.timeout(30_000),
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'User-Agent': setupUserAgent },
        body: JSON.stringify({ code }),
      })
      claim = await responseData(claimResponse, 'Gemcp setup claim')
    }
    if (!claim.enrollment_id || !claim.project_id || !isExpectedScopes(claim.scopes) || !/^gemcp(?:-[a-z0-9-]+)?$/.test(claim.server_name || '')) throw new Error('Gemcp claim returned invalid enrollment metadata')
    if (!isExpectedPiConfig(claim.pi_config, claim.agent_token)) {
      throw new Error('Gemcp claim returned an invalid Pi MCP configuration')
    }
    const mcpURL = new URL(claim.pi_config?.url || '')
    if (mcpURL.origin !== trustedOrigin || mcpURL.pathname !== '/mcp' || mcpURL.search || mcpURL.hash || mcpURL.username || mcpURL.password) {
      throw new Error('Gemcp claim returned an unexpected MCP endpoint')
    }

    config.mcpServers[claim.server_name] = claim.pi_config
    await atomicWrite(configPath, `${JSON.stringify(config, null, 2)}\n`, 0o600)

    const helper = await import(`${pathToFileURL(cliPath).href}?install=${Date.now()}`)
    const verification = await helper.verifyConfiguredServer(configPath, claim.server_name)
    const verifiedReceipt = {
      version: 1,
      status: 'verified',
      setup_origin: trustedOrigin,
      enrollment_id: claim.enrollment_id,
      project_id: claim.project_id,
      project_name: claim.project_name,
      server_name: claim.server_name,
      scopes: claim.scopes,
      mcp_url: claim.pi_config.url,
      config_path: configPath,
      tool_count: verification.toolCount,
      checks: verification.checks,
      verified_at: new Date().toISOString(),
    }
    await atomicWrite(receiptPath, `${JSON.stringify(verifiedReceipt, null, 2)}\n`, 0o600)

    const completeResponse = await fetch(`${trustedOrigin}/api/v1/agent-enrollments/complete`, {
      method: 'POST', redirect: 'error', signal: AbortSignal.timeout(30_000),
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'User-Agent': setupUserAgent },
      body: JSON.stringify({
        code,
        client: `pi-mcp-adapter/${adapterPackage.version || 'unknown'}`,
        tool_count: verification.toolCount,
        checks: verification.checks,
      }),
    })
    const completed = await responseData(completeResponse, 'Gemcp setup completion')
    if (completed.enrollment?.id !== claim.enrollment_id || completed.enrollment?.status !== 'completed' || completed.token?.status !== 'active' ||
        JSON.stringify(completed.token?.scopes) !== JSON.stringify(claim.scopes) || !completed.token?.id || !completed.token?.prefix) {
      throw new Error('Gemcp setup completion returned invalid activation metadata')
    }
    const receipt = {
      version: 1,
      status: 'completed',
      setup_origin: trustedOrigin,
      enrollment_id: claim.enrollment_id,
      project_id: claim.project_id,
      project_name: claim.project_name,
      server_name: claim.server_name,
      scopes: claim.scopes,
      token_id: completed.token?.id,
      token_prefix: completed.token?.prefix,
      token_expires_at: completed.token?.expires_at || null,
      config_path: configPath,
      tool_count: verification.toolCount,
      checks: verification.checks,
      completed_at: new Date().toISOString(),
    }
    await atomicWrite(receiptPath, `${JSON.stringify(receipt, null, 2)}\n`, 0o600)
    printSuccess({ serverName: claim.server_name, configPath, cliPath, verification })
  } finally {
    await lock?.close().catch(() => {})
    await fs.rm(lockPath, { force: true }).catch(() => {})
  }
}

// GEMCP_PI_SETUP_INSTALLER_V1
main().catch(error => {
  process.stderr.write(`Gemcp Pi setup failed: ${error instanceof Error ? error.message : String(error)}\n`)
  process.exitCode = 1
})
