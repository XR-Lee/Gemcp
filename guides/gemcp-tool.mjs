#!/usr/bin/env node

// GEMCP_TOOL_HELPER_V1
import fs from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

const protocolVersion = '2025-11-25'
const setupUserAgent = 'Gemcp-Pi-Setup/1'
const requiredTools = [
  'get_usage_guide',
  'get_research_workspace',
  'update_research_workspace',
  'get_next_actions',
  'close_run',
  'get_experiment_catalog',
  'record_experiment_catalog',
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
  'read_artifact',
]

class MCPConnection {
  constructor(url, token) {
    this.url = url
    this.token = token
    this.id = 0
    this.sessionID = ''
  }

  async request(method, params = {}, notification = false) {
    const requestID = notification ? undefined : ++this.id
    const payload = { jsonrpc: '2.0', method, params }
    if (!notification) payload.id = requestID
    const headers = {
      Authorization: `Bearer ${this.token}`,
      Accept: 'application/json, text/event-stream',
      'Content-Type': 'application/json',
      'MCP-Protocol-Version': protocolVersion,
      'User-Agent': setupUserAgent,
    }
    if (this.sessionID) headers['Mcp-Session-Id'] = this.sessionID
    const response = await fetch(this.url, {
      method: 'POST',
      headers,
      body: JSON.stringify(payload),
      redirect: 'error',
      signal: AbortSignal.timeout(30_000),
    })
    const nextSessionID = response.headers.get('mcp-session-id')
    if (nextSessionID) this.sessionID = nextSessionID
    if (notification) {
      if (!response.ok) throw new Error(`MCP ${method} failed with HTTP ${response.status}`)
      return undefined
    }
    const text = await response.text()
    if (!response.ok) throw new Error(`MCP ${method} failed with HTTP ${response.status}`)
    const messages = parseMCPResponse(text, response.headers.get('content-type') || '')
    const message = messages.find(item => item && item.id === requestID)
    if (!message) throw new Error(`MCP ${method} returned no matching response`)
    if (message.error) throw new Error(`MCP ${method} failed: ${message.error.message || 'unknown error'}`)
    return message.result
  }

  async initialize() {
    await this.request('initialize', {
      protocolVersion,
      capabilities: {},
      clientInfo: { name: 'gemcp-pi-setup', version: '1.0.0' },
    })
    await this.request('notifications/initialized', {}, true)
  }

  async close() {
    if (!this.sessionID) return
    try {
      await fetch(this.url, {
        method: 'DELETE',
        headers: {
          Authorization: `Bearer ${this.token}`,
          'MCP-Protocol-Version': protocolVersion,
          'Mcp-Session-Id': this.sessionID,
          'User-Agent': setupUserAgent,
        },
        redirect: 'error',
        signal: AbortSignal.timeout(10_000),
      })
    } catch {
      // Session cleanup is best effort; the server also expires idle sessions.
    }
  }
}

function parseMCPResponse(text, contentType) {
  if (contentType.toLowerCase().includes('text/event-stream')) {
    const messages = []
    for (const block of text.split(/\r?\n\r?\n/)) {
      const data = block.split(/\r?\n/)
        .filter(line => line.startsWith('data:'))
        .map(line => line.slice(5).trimStart())
        .join('\n')
      if (!data) continue
      messages.push(JSON.parse(data))
    }
    return messages
  }
  return text.trim() ? [JSON.parse(text)] : []
}

function resolveAgentDir() {
  const configured = process.env.PI_CODING_AGENT_DIR?.trim()
  if (!configured) return path.join(os.homedir(), '.pi', 'agent')
  if (configured === '~') return os.homedir()
  if (configured.startsWith('~/')) return path.join(os.homedir(), configured.slice(2))
  return path.resolve(configured)
}

async function loadConfiguredServer(configPath, serverName) {
  const config = JSON.parse(await fs.readFile(configPath, 'utf8'))
  const definition = config?.mcpServers?.[serverName]
  if (!definition || definition.auth !== 'bearer' || typeof definition.bearerToken !== 'string' || !definition.bearerToken) {
    throw new Error(`Gemcp server ${serverName} is not installed in ${configPath}`)
  }
  if (typeof definition.url !== 'string' || !definition.url.startsWith('https://')) {
    throw new Error(`Gemcp server ${serverName} has an invalid HTTPS URL`)
  }
  return definition
}

async function callChecked(connection, name, args = {}) {
  const result = await connection.request('tools/call', { name, arguments: args })
  if (result?.isError) throw new Error(`${name} returned a tool error`)
  return result
}

export async function verifyConfiguredServer(configPath, serverName) {
  const definition = await loadConfiguredServer(configPath, serverName)
  const connection = new MCPConnection(definition.url, definition.bearerToken)
  try {
    await connection.initialize()
    const listed = await connection.request('tools/list')
    const names = new Set((listed?.tools || []).map(tool => tool.name))
    for (const required of requiredTools) {
      if (!names.has(required)) throw new Error(`Gemcp tool is missing: ${required}`)
    }
    const guide = await callChecked(connection, 'get_usage_guide')
    const guideJSON = JSON.stringify(guide?.structuredContent || guide?.content || '')
    if (!guideJSON.includes('get_project_options') || !guideJSON.includes('explicit human approval')) {
      throw new Error('Gemcp usage guide validation failed')
    }
    await callChecked(connection, 'get_project_options')
    await callChecked(connection, 'get_project_cost')
    return { toolCount: names.size, checks: ['tools', 'guide', 'options', 'cost'] }
  } finally {
    await connection.close()
  }
}

export async function callConfiguredTool(configPath, serverName, toolName, args = {}) {
  const definition = await loadConfiguredServer(configPath, serverName)
  const connection = new MCPConnection(definition.url, definition.bearerToken)
  try {
    await connection.initialize()
    return await callChecked(connection, toolName, args)
  } finally {
    await connection.close()
  }
}

async function main() {
  const [serverName, toolName, argsJSON = '{}'] = process.argv.slice(2)
  if (!serverName || !toolName) {
    throw new Error('usage: gemcp-tool.mjs SERVER TOOL [ARGS_JSON]')
  }
  const args = JSON.parse(argsJSON)
  if (!args || typeof args !== 'object' || Array.isArray(args)) throw new Error('ARGS_JSON must be an object')
  const configPath = path.join(resolveAgentDir(), 'mcp.json')
  const result = await callConfiguredTool(configPath, serverName, toolName, args)
  process.stdout.write(`${JSON.stringify(result?.structuredContent ?? result?.content ?? result, null, 2)}\n`)
}

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : ''
if (invokedPath === import.meta.url) {
  main().catch(error => {
    process.stderr.write(`Gemcp tool failed: ${error instanceof Error ? error.message : String(error)}\n`)
    process.exitCode = 1
  })
}
