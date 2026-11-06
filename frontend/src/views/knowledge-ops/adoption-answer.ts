import { unwrapFinalAnswerWrappers } from '../../utils/finalAnswer'
import { Lexer } from 'marked'

const referenceTag = /^<(kb|web)\b((?:[^<>"']|"[^"]*"|'[^']*')*)>/i
const attribute = /(?:^|\s)([\w-]+)(?:\s*=\s*(?:"[^"]*"|'[^']*'|[^\s"'=<>`]+))?/g

function stabilizeLeadingCode(source: string): string {
  if (!/^(?:[ \t]*\r?\n)*(?: {4}|\t)/.test(source)) return source
  let offset = 0
  for (const token of Lexer.lex(source)) {
    if (token.type === 'space') { offset += token.raw.length; continue }
    if (token.type === 'code' && token.codeBlockStyle === 'indented') {
      const text = String(token.text)
      const width = Math.max(2, ...Array.from(text.matchAll(/`+/g), (run) => run[0].length)) + 1
      const fence = '`'.repeat(width)
      return `${source.slice(0, offset)}${fence}\n${text}\n${fence}\n${source.slice(offset + token.raw.length)}`
    }
    break
  }
  return source
}

function codeContainer() {
  const stack: ('quote' | number)[] = []
  return (raw: string, inFence: boolean) => {
    let line = raw
    let changed = false
    if (!line.trim()) return { line, changed }
    for (let i = 0; i < stack.length; i++) {
      const part = stack[i]
      const prefix = part === 'quote' ? line.match(/^ {0,3}> ?/)?.[0] : line.startsWith(' '.repeat(part)) ? ' '.repeat(part) : ''
      if (!prefix) { stack.splice(i); changed = true; break }
      line = line.slice(prefix.length)
    }
    if (!inFence || changed) {
      for (;;) {
        const quote = line.match(/^ {0,3}> ?/)
        const marker = line.match(/^ {0,3}(?:[-+*]|\d{1,9}[.)])[ \t]+/)
        const prefix = quote?.[0] || marker?.[0]
        if (!prefix) break
        stack.push(quote ? 'quote' : prefix.length)
        line = line.slice(prefix.length)
        changed = true
      }
    }
    return { line, changed }
  }
}

function unwrapDraft(content: string): string {
  // Unlike rendering-only unwrapping, publication must preserve leading code
  // indentation until it is made stable against the FAQ service's trim step.
  let body = content
  for (let i = 0; i < 3; i++) {
    const tag = body.match(/^\s*<(answer|final_answer|final-answer)\b[^>]*>([\s\S]*?)<\/\1>\s*$/i)
    if (!tag) break
    body = tag[2]
  }
  const fenced = body.match(/^\s*```(?:final_answer|answer)[ \t]*\n([\s\S]*?)\n```\s*$/i)
  if (fenced) body = fenced[1]
  body = body.replace(/^\s*(?:final\s*answer|最终答案|答案|答)[ \t]*[:：][ \t]*/i, '')
  return unwrapFinalAnswerWrappers(stabilizeLeadingCode(body))
}

function stripOutsideInlineCode(prose: string): string {
  let result = ''
  for (let i = 0; i < prose.length;) {
    if (prose[i] === '\\') {
      result += prose.slice(i, i + 2)
      i += 2
      continue
    }
    if (prose[i] === '[' && prose[i - 1] !== '[') {
      const citation = prose.slice(i).match(/^\[c\d{3,}(?:\s*[,;]\s*c\d{3,})*\]/)
      // Keep literal Markdown links and definitions. Wiki publication blocks
      // remaining handles because the Wiki service would strip them on save.
      if (citation && !/^\s*[([:\]]/.test(prose.slice(i + citation[0].length))) {
        result = result.replace(/[ \t]+$/, '')
        i += citation[0].length
        continue
      }
    }
    if (prose[i] === '<') {
      const tag = prose.slice(i).match(referenceTag)
      if (tag) {
        const names = Array.from(tag[2].matchAll(attribute), (match) => match[1].toLowerCase())
        const known = tag[1].toLowerCase() === 'kb' ? ['doc', 'chunk_id', 'chunkid', 'kb_id', 'kbid'] : ['url']
        if (!names.some((name) => known.includes(name))) result += tag[0]
        i += tag[0].length
        continue
      }
    }
    if (prose[i] === '`') {
      const width = prose.slice(i).match(/^`+/)![0].length
      const closingRuns = /`+/g
      closingRuns.lastIndex = i + width
      let closing: RegExpExecArray | null
      let end = i + width
      while ((closing = closingRuns.exec(prose))) {
        if (closing[0].length === width) { end = closing.index + width; break }
      }
      result += prose.slice(i, end)
      i = end
      continue
    }
    result += prose[i++]
  }
  return result
}

// Normalize the editable publication draft only. Original answers and evidence
// retain the citation protocol used by the chat UI and diagnostic audit.
export function prepareAdoptionAnswer(content: string): string {
  const source = unwrapDraft(content)
  let result = ''
  let prose = ''
  let fence = ''
  const container = codeContainer()
  const flush = () => { result += stripOutsideInlineCode(prose); prose = '' }
  for (const line of source.match(/[^\n]*\n|[^\n]+$/g) || []) {
    const block = container(line, !!fence)
    if (block.changed) { flush(); fence = '' }
    const delimiter = block.line.match(/^ {0,3}(`{3,}|~{3,})([^\n]*)/)
    if (fence) {
      result += line
      if (delimiter && delimiter[1][0] === fence[0] && delimiter[1].length >= fence.length && !delimiter[2].trim()) fence = ''
    } else if (delimiter && (delimiter[1][0] !== '`' || !delimiter[2].includes('`'))) {
      flush()
      fence = delimiter[1]
      result += line
    } else if (!line.trim()) {
      flush()
      result += line
    } else if (!prose && /^(?: {4}|\t)/.test(block.line)) {
      result += line
    } else {
      prose += line
    }
  }
  flush()
  return result.trim()
}
