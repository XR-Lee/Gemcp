import { describe, expect, it } from 'vitest'
import { panelPath, readPanelSelection } from './panelLocation'

describe('panel location', () => {
  it('reads a study deep link and ignores an unknown view', () => {
    expect(readPanelSelection('?project=project-1&study=study-1&view=research&lang=en')).toEqual({
      projectID: 'project-1',
      studyID: 'study-1',
      view: 'research',
    })
    expect(readPanelSelection('?view=not-a-view&project=%20%20&study=%20')).toEqual({
      projectID: '',
      studyID: '',
      view: '',
    })
  })

  it('writes the selection without dropping unrelated parameters', () => {
    expect(panelPath('/?lang=en#graph', { projectID: 'project-1', studyID: 'study-1', view: 'research' })).toBe(
      '/?lang=en&project=project-1&study=study-1&view=research#graph',
    )
    expect(panelPath('/?project=old&study=old&view=experiments', { projectID: 'project-1', studyID: '', view: 'experiments' })).toBe(
      '/?project=project-1&view=experiments',
    )
  })
})
