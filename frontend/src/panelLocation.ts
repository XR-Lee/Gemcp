export const panelViews = [
  'research',
  'experiments',
  'diagnostics',
  'images',
  'finance',
  'projects',
  'agents',
  'nodes',
  'provider',
  'notifications',
] as const

export type PanelView = (typeof panelViews)[number]

export type PanelSelection = {
  projectID: string
  studyID: string
  view: PanelView | ''
}

export function readPanelSelection(search: string): PanelSelection {
  const params = new URLSearchParams(search.startsWith('?') ? search.slice(1) : search)
  const view = params.get('view')?.trim() ?? ''
  return {
    projectID: params.get('project')?.trim() ?? '',
    studyID: params.get('study')?.trim() ?? '',
    view: (panelViews as readonly string[]).includes(view) ? (view as PanelView) : '',
  }
}

// panelPath keeps unrelated query parameters and the hash, and writes the
// Owner console selection as project, study, and view.
export function panelPath(current: string, selection: { projectID?: string; studyID?: string; view?: string }): string {
  const url = new URL(current || '/', 'http://gemcp.local')
  const set = (key: string, value?: string) => {
    const trimmed = value?.trim() ?? ''
    if (trimmed) url.searchParams.set(key, trimmed)
    else url.searchParams.delete(key)
  }
  set('project', selection.projectID)
  set('study', selection.studyID)
  set('view', selection.view)
  const query = url.searchParams.toString()
  return `${url.pathname}${query ? `?${query}` : ''}${url.hash}`
}
