export interface DiffLine {
  hunkHeader?: string
  base: number | null
  head: number | null
  kind: 'context' | 'added' | 'removed'
  text: string
}

export interface LineRange {
  side: 'base' | 'head'
  start: number
  end: number
}
