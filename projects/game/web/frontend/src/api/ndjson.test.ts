import { describe, expect, it } from 'vitest'
import { ndjsonLines } from './conversation.js'

// chunksToStream builds a ReadableStream emitting the given byte chunks in
// order — the fetch response.body shape sendStream consumes.
function chunksToStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk)
      controller.close()
    },
  })
}

function bytes(s: string): Uint8Array {
  return new TextEncoder().encode(s)
}

async function collect(chunks: Uint8Array[]): Promise<string[]> {
  const lines: string[] = []
  for await (const line of ndjsonLines(chunksToStream(chunks))) lines.push(line)
  return lines
}

describe('ndjsonLines', () => {
  it('reassembles a line split across two chunks (half line)', async () => {
    const lines = await collect([
      bytes('{"turnStart":{}}\n{"delta":{"index":0,"text":"he'),
      bytes('llo"}}\n'),
    ])
    expect(lines).toEqual([
      '{"turnStart":{}}',
      '{"delta":{"index":0,"text":"hello"}}',
    ])
  })

  it('splits glued lines arriving in one chunk (粘包)', async () => {
    const lines = await collect([
      bytes('{"queued":{"position":1}}\n{"turnStart":{}}\n'),
    ])
    expect(lines).toEqual(['{"queued":{"position":1}}', '{"turnStart":{}}'])
  })

  it('keeps multi-byte UTF-8 sequences intact across chunk boundaries', async () => {
    // The first chunk ends inside the multi-byte "地" rune; the decoder's
    // stream mode must hold the partial sequence until the next chunk.
    const line = bytes('{"delta":{"index":0,"text":"地雷"}}\n')
    const cut = line.length - 6
    const lines = await collect([line.slice(0, cut), line.slice(cut)])
    expect(lines).toEqual(['{"delta":{"index":0,"text":"地雷"}}'])
  })

  it('ignores a trailing fragment without a terminating newline', async () => {
    const lines = await collect([bytes('{"turnStart":{}}\n{"partial"')])
    expect(lines).toEqual(['{"turnStart":{}}'])
  })
})
