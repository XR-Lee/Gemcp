export type ParsedSSHTarget = {
  user: string
  host: string
  port: number
  password?: string
}

const flagsWithValue = new Set([
  'b', 'B', 'c', 'D', 'E', 'F', 'i', 'I', 'J', 'l', 'L', 'm', 'O', 'o', 'p', 'P', 'R', 'S', 'W', 'w',
])

export function parseSSHTarget(raw: string): ParsedSSHTarget | null {
  const text = raw.replace(/\u00a0/g, ' ').trim()
  if (!text) return null
  const password = extractPassword(text)
  const line = findSSHLine(text)
  if (!line) return null
  const parsed = parseSSHLine(line)
  if (!parsed) return null
  return password ? { ...parsed, password } : parsed
}

export function formatSSHTarget(target: Pick<ParsedSSHTarget, 'user' | 'host' | 'port'>): string {
  const host = target.host.includes(':') && !target.host.startsWith('[') ? `[${target.host}]` : target.host
  return `${target.user}@${host}:${target.port}`
}

export function suggestedSSHLabel(target: Pick<ParsedSSHTarget, 'host'>): string {
  const host = target.host.replace(/^\[|\]$/g, '')
  const first = host.split('.')[0]?.trim()
  return first || host
}

function extractPassword(text: string): string | undefined {
  const match = text.match(/(?:^|\n)\s*(?:password|passwd|pwd|密码|口令)\s*[:：=]\s*(\S+)/i)
  const value = match?.[1]?.trim()
  return value || undefined
}

function findSSHLine(text: string): string {
  const lines = text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  const sshLine = lines.find((line) => /^(ssh|sftp|scp)(\s|:\/\/)/i.test(line))
  if (sshLine) return sshLine
  const destination = lines.find((line) => /@/.test(line) && !/^(password|passwd|pwd|密码|口令)\b/i.test(line))
  return destination ?? lines[0] ?? text
}

function parseSSHLine(line: string): Omit<ParsedSSHTarget, 'password'> | null {
  const trimmed = line.replace(/^[$#]\s+/, '').trim()
  if (/^(ssh|sftp):\/\//i.test(trimmed)) {
    return destinationTarget(parseDestination(trimmed), {})
  }
  const tokens = tokenize(trimmed)
  if (tokens.length === 0) return null
  let index = /^(ssh|sftp|scp)$/i.test(tokens[0]) ? 1 : 0
  let user: string | undefined
  let port: number | undefined
  let destination: string | undefined
  while (index < tokens.length) {
    const token = tokens[index]
    if (token.startsWith('-') && token !== '-') {
      const body = token.replace(/^--?/, '')
      const combined = body.match(/^([pPl])(.+)$/)
      if (combined && !body.includes('=')) {
        if (combined[1].toLowerCase() === 'l') user = combined[2]
        else port = Number(combined[2])
        index += 1
        continue
      }
      const name = (body.split('=')[0] ?? '').toLowerCase()
      const short = name[0] ?? ''
      if (flagsWithValue.has(short) || flagsWithValue.has(name)) {
        const value = token.includes('=') ? token.slice(token.indexOf('=') + 1) : tokens[index + 1] ?? ''
        if (!token.includes('=')) index += 1
        if (short === 'p' || name === 'port') port = Number(value)
        if (short === 'l' || name === 'user') user = value
        if (short === 'o' || name === 'option') {
          const option = value.match(/^(port|user)\s*=\s*(.+)$/i)
          if (option?.[1].toLowerCase() === 'port') port = Number(option[2])
          if (option?.[1].toLowerCase() === 'user') user = option[2].trim()
        }
        index += 1
        continue
      }
      index += 1
      continue
    }
    if (!destination) destination = token
    index += 1
  }
  if (!destination && tokens.length === 1) destination = tokens[0]
  return destinationTarget(destination ? parseDestination(destination) : null, { user, port })
}

function destinationTarget(
  destination: { user?: string; host: string; port?: number } | null,
  extra: { user?: string; port?: number },
): Omit<ParsedSSHTarget, 'password'> | null {
  if (!destination?.host) return null
  const user = (extra.user || destination.user || '').trim()
  const port = extra.port && extra.port > 0 ? extra.port : destination.port && destination.port > 0 ? destination.port : 22
  if (!user || !Number.isInteger(port) || port < 1 || port > 65535) return null
  if (/[\s/@]/.test(destination.host) || destination.host.startsWith('-')) return null
  return { user, host: destination.host, port }
}

function parseDestination(raw: string): { user?: string; host: string; port?: number } | null {
  let value = raw.trim().replace(/[,;]+$/, '')
  if (!value) return null
  if (/^(ssh|sftp):\/\//i.test(value)) {
    try {
      const url = new URL(value)
      const host = url.hostname.replace(/^\[|\]$/g, '')
      if (!host) return null
      return {
        user: url.username ? decodeURIComponent(url.username) : undefined,
        host,
        port: url.port ? Number(url.port) : undefined,
      }
    } catch {
      return null
    }
  }
  let user: string | undefined
  const at = value.lastIndexOf('@')
  if (at > 0) {
    user = value.slice(0, at)
    value = value.slice(at + 1)
  }
  if (value.startsWith('[')) {
    const close = value.indexOf(']')
    if (close < 1) return null
    const host = value.slice(1, close)
    const rest = value.slice(close + 1)
    const port = rest.startsWith(':') ? Number(rest.slice(1)) : undefined
    return host ? { user, host, port } : null
  }
  const colon = value.lastIndexOf(':')
  if (colon > 0 && /^\d+$/.test(value.slice(colon + 1)) && !value.includes('::')) {
    return { user, host: value.slice(0, colon), port: Number(value.slice(colon + 1)) }
  }
  return value ? { user, host: value } : null
}

function tokenize(line: string): string[] {
  const tokens: string[] = []
  let current = ''
  let quote: '"' | "'" | null = null
  for (const character of line) {
    if (quote) {
      if (character === quote) quote = null
      else current += character
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      continue
    }
    if (/\s/.test(character)) {
      if (current) {
        tokens.push(current)
        current = ''
      }
      continue
    }
    current += character
  }
  if (current) tokens.push(current)
  return tokens
}
