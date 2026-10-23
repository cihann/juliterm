import { describe, expect, it } from 'vitest'
import { cellAt, linearSelection } from './touchCells'

const box = { left: 10, top: 20, width: 800, height: 480 } // 80x24 → 10x20 cells

describe('cellAt', () => {
  it('maps a point to its cell', () => {
    expect(cellAt(10, 20, box, 80, 24)).toEqual({ col: 0, row: 0 })
    expect(cellAt(10 + 25, 20 + 45, box, 80, 24)).toEqual({ col: 2, row: 2 })
  })

  it('clamps points outside the screen', () => {
    expect(cellAt(0, 0, box, 80, 24)).toEqual({ col: 0, row: 0 })
    expect(cellAt(5000, 5000, box, 80, 24)).toEqual({ col: 79, row: 23 })
  })
})

describe('linearSelection', () => {
  it('selects forward, inclusive', () => {
    expect(linearSelection({ col: 2, row: 1 }, { col: 5, row: 1 }, 80)).toEqual({ col: 2, row: 1, length: 4 })
  })

  it('normalises a reverse drag', () => {
    expect(linearSelection({ col: 5, row: 3 }, { col: 78, row: 1 }, 80)).toEqual({ col: 78, row: 1, length: 2 + 80 + 6 })
  })

  it('selects a single cell', () => {
    expect(linearSelection({ col: 4, row: 4 }, { col: 4, row: 4 }, 80)).toEqual({ col: 4, row: 4, length: 1 })
  })
})
