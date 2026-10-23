// Map touch positions to terminal cells, for selecting text with a finger.

export interface Cell {
  col: number
  row: number
}

export interface ScreenBox {
  left: number
  top: number
  width: number
  height: number
}

/** The visible cell under a point, clamped to the screen. */
export function cellAt(x: number, y: number, box: ScreenBox, cols: number, rows: number): Cell {
  const cellW = box.width / cols
  const cellH = box.height / rows
  const col = Math.floor((x - box.left) / cellW)
  const row = Math.floor((y - box.top) / cellH)
  return {
    col: Math.min(cols - 1, Math.max(0, col)),
    row: Math.min(rows - 1, Math.max(0, row)),
  }
}

/**
 * The linear selection from one cell to another, inclusive, in the argument
 * order xterm's `select(column, row, length)` takes; works for reverse drags.
 */
export function linearSelection(a: Cell, b: Cell, cols: number): { col: number, row: number, length: number } {
  const ia = a.row * cols + a.col
  const ib = b.row * cols + b.col
  const start = Math.min(ia, ib)
  const end = Math.max(ia, ib)
  return { col: start % cols, row: Math.floor(start / cols), length: end - start + 1 }
}
