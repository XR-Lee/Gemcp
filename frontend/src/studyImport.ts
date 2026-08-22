import type { Locale } from './i18n'

const githubSSHURL = /^git@github\.com:([A-Za-z0-9_.-]+)\/([A-Za-z0-9_.-]+?)(?:\.git)?$/

export type StudyImportDraft = {
  name: string
  question: string
  summary: string
}

export function repositoryNameFromSSHURL(url: string) {
  return githubSSHURL.exec(url.trim())?.[2] ?? ''
}

export function draftStudyFromRepository(
  input: { name: string; ssh_url: string; default_branch?: string },
  locale: Locale = 'en',
): StudyImportDraft {
  const name = input.name.trim() || repositoryNameFromSSHURL(input.ssh_url)
  const branch = input.default_branch?.trim() || 'main'
  const question = locale === 'zh'
    ? `这个 Study 要从 ${name} 学到什么？`
    : `What should this Study learn from ${name}?`
  return {
    name,
    question,
    summary: `${input.ssh_url.trim()} @ ${branch}`,
  }
}
