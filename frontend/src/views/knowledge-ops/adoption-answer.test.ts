import assert from 'node:assert/strict'
import { test } from 'node:test'
import { prepareAdoptionAnswer } from './adoption-answer.ts'

test('adoption drafts unwrap the final answer and remove citation protocol without rewriting its Markdown', () => {
  const raw = '<answer>\n### 核心成果\n\n* **信息提取**：完整正文<kb doc="材料.md" chunk_id="c1" kb_id="k1" />。\n* 规则判断<web url="https://example.com" />。\n</answer>'
  const expected = '### 核心成果\n\n* **信息提取**：完整正文。\n* 规则判断。'
  assert.equal(prepareAdoptionAnswer(raw), expected)
  assert.equal(prepareAdoptionAnswer(expected), expected)
})

test('only known reference protocols are removed and normal links, HTML and quoted delimiters survive', () => {
  const input = '正文<KB DOC="材料 > 说明.md" chunkId="c"/><web url=\'https://example.com\'/>。\n[官网](https://example.com)\n<kbd>Enter</kbd> &lt;kb doc="literal"/&gt;'
  assert.equal(prepareAdoptionAnswer(input), '正文。\n[官网](https://example.com)\n<kbd>Enter</kbd> &lt;kb doc="literal"/&gt;')
  assert.equal(prepareAdoptionAnswer('<kb example="literal"/>'), '<kb example="literal"/>')
  assert.equal(prepareAdoptionAnswer('<kb example="doc=literal"/>'), '<kb example="doc=literal"/>')
  assert.equal(prepareAdoptionAnswer('说明\\<kb doc="literal"/>'), '说明\\<kb doc="literal"/>')
  assert.equal(prepareAdoptionAnswer('正文<kb doc="`材料`"/>。'), '正文。')
  assert.equal(prepareAdoptionAnswer('正文<kb chunkId="c">。'), '正文。')
  assert.equal(prepareAdoptionAnswer('正文<kb doc/>。'), '正文。')
})

test('literal reference examples in fenced, indented and inline code remain intact', () => {
  const input = [
    'Example `<kb doc="literal.md" chunk_id="example"/>`.',
    '````markdown',
    '```',
    '<kb doc="literal.md"/>',
    '```',
    '````',
    '~~~html',
    '<web url="https://example.com"/>',
    '~~~',
    '    <kb doc="indented.md"/>',
    'Body<kb doc="real.md"/>.',
  ].join('\n')
  assert.equal(prepareAdoptionAnswer(input), input.replace('Body<kb doc="real.md"/>.', 'Body.'))
  assert.equal(prepareAdoptionAnswer('Code ``one ` <kb doc="literal"/>``.'), 'Code ``one ` <kb doc="literal"/>``.')
  assert.equal(prepareAdoptionAnswer('Unclosed ` then <kb doc="actual.md"/>.'), 'Unclosed ` then .')
  assert.equal(prepareAdoptionAnswer('正文\n    <kb doc="actual.md"/>'), '正文')
})

test('citation-only content cannot become a publishable FAQ answer', () => {
  assert.equal(prepareAdoptionAnswer(' <kb doc="材料.md" chunk_id="c"/>\n<web url="https://example.com"/> '), '')
})

test('code examples in list and quote containers survive while following prose is cleaned', () => {
  for (const code of [
    '- ```xml\n  <kb doc="literal.md"/>\n  ```',
    '1. ~~~html\n   <web url="https://example.com"/>\n   ~~~',
    'Example:\n\n>     <kb doc="literal.md"/>',
    '> ```html\n> <kb doc="literal.md"/>\n> ```',
    '- > ```xml\n  > <kb doc="literal.md"/>\n  > ```',
    '> - ```xml\n>   <kb doc="literal.md"/>\n>   ```',
  ]) {
    assert.equal(prepareAdoptionAnswer(`${code}\n\n正文<kb doc="actual.md"/>。`), `${code}\n\n正文。`)
  }
  assert.equal(prepareAdoptionAnswer('> ```html\n> <kb doc="literal.md"/>\n正文<kb doc="actual.md"/>。'), '> ```html\n> <kb doc="literal.md"/>\n正文。')
})

test('a leading indented code example is converted to a stable fenced draft before FAQ trimming', () => {
  const raw = '    <kb doc="literal.md"/>'
  const prepared = prepareAdoptionAnswer(raw)
  assert.match(prepared, /^```\n<kb doc="literal.md"\/>\n```$/)
  assert.equal(prepareAdoptionAnswer(prepared), prepared)
  assert.equal(prepareAdoptionAnswer(`<answer>\n${raw}\n</answer>`), prepared)
  assert.equal(prepareAdoptionAnswer(`Final Answer:\n${raw}`), prepared)
})
