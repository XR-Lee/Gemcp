import { describe, expect, it } from 'vitest'
import { draftStudyFromRepository, repositoryNameFromSSHURL } from './studyImport'

describe('studyImport', () => {
  it('derives a repository name from a GitHub SSH URL', () => {
    expect(repositoryNameFromSSHURL('git@github.com:research/dynamic-point-mamba.git')).toBe('dynamic-point-mamba')
    expect(repositoryNameFromSSHURL('git@github.com:XR-Lee/Gemcp')).toBe('Gemcp')
    expect(repositoryNameFromSSHURL('https://github.com/research/dynamic-point-mamba')).toBe('')
  })

  it('drafts a Study from an existing repository', () => {
    expect(draftStudyFromRepository({
      name: 'dynamic-point-mamba',
      ssh_url: 'git@github.com:research/dynamic-point-mamba.git',
      default_branch: 'main',
    }, 'en')).toEqual({
      name: 'dynamic-point-mamba',
      question: 'What should this Study learn from dynamic-point-mamba?',
      summary: 'git@github.com:research/dynamic-point-mamba.git @ main',
    })
    expect(draftStudyFromRepository({
      name: 'dynamic-point-mamba',
      ssh_url: 'git@github.com:research/dynamic-point-mamba.git',
    }, 'zh').question).toContain('dynamic-point-mamba')
  })
})
