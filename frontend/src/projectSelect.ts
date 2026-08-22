import type { Project } from './api'

export function pickDefaultProjectID(projects: Project[], current = '') {
  if (projects.some((project) => project.id === current)) return current
  const newest = [...projects].sort((left, right) => {
    const created = (right.created_at ?? '').localeCompare(left.created_at ?? '')
    if (created !== 0) return created
    return (right.updated_at ?? '').localeCompare(left.updated_at ?? '')
  })[0]
  return newest?.id ?? ''
}
