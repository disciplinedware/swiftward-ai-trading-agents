import { describe, it, expect, beforeEach, vi } from 'vitest'
import { buildJsonRpcRequest, mcpCall, McpError, resetRequestId } from './mcp-client'

/** Wrap data in MCP ToolResult format as returned by the Go server. */
function toolResultResponse(data: unknown, id = 1) {
  return new Response(
    JSON.stringify({
      jsonrpc: '2.0',
      id,
      result: {
        content: [{ type: 'text', text: JSON.stringify(data) }],
      },
    }),
    { status: 200, headers: { 'Content-Type': 'application/json' } },
  )
}

beforeEach(() => {
  resetRequestId()
  vi.restoreAllMocks()
})

describe('buildJsonRpcRequest', () => {
  it('builds a valid JSON-RPC 2.0 request', () => {
    const req = buildJsonRpcRequest('tools/call', { name: 'trade/get_portfolio', arguments: { limit: 10 } })
    expect(req).toEqual({
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: { name: 'trade/get_portfolio', arguments: { limit: 10 } },
    })
  })

  it('defaults params to empty object', () => {
    const req = buildJsonRpcRequest('tools/list')
    expect(req.params).toEqual({})
  })

  it('increments id across calls', () => {
    const r1 = buildJsonRpcRequest('a')
    const r2 = buildJsonRpcRequest('b')
    expect(r2.id).toBe(r1.id + 1)
  })
})

describe('mcpCall', () => {
  it('sends tools/call with name+arguments and unwraps ToolResult', async () => {
    const mockData = { cash: '10000', positions: [] }
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(toolResultResponse(mockData))

    const result = await mcpCall('/mcp/trading', 'trade/get_portfolio')
    expect(result).toEqual(mockData)

    const sentBody = JSON.parse(fetchSpy.mock.calls[0][1]!.body as string)
    expect(sentBody.method).toBe('tools/call')
    expect(sentBody.params).toEqual({ name: 'trade/get_portfolio', arguments: {} })
  })

  it('sends X-Agent-ID header when agentId is provided', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(toolResultResponse({}))

    await mcpCall('/mcp/trading', 'trade/get_portfolio', {}, 'alpha-1')

    expect(fetch).toHaveBeenCalledWith('/mcp/trading', expect.objectContaining({
      headers: {
        'Content-Type': 'application/json',
        'X-Agent-ID': 'alpha-1',
      },
    }))
  })

  it('passes tool arguments in the request body', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      toolResultResponse({ trades: [], count: 0 }),
    )

    await mcpCall('/mcp/trading', 'trade/get_history', { limit: 25 })

    const sentBody = JSON.parse(fetchSpy.mock.calls[0][1]!.body as string)
    expect(sentBody.params).toEqual({ name: 'trade/get_history', arguments: { limit: 25 } })
  })

  it('throws McpError on JSON-RPC error response', async () => {
    const errorBody = { jsonrpc: '2.0', id: 1, error: { code: -32600, message: 'Invalid request' } }
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(JSON.stringify(errorBody), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(errorBody), { status: 200 }))

    await expect(mcpCall('/mcp/trading', 'trade/get_portfolio')).rejects.toThrow(McpError)
    await expect(mcpCall('/mcp/trading', 'trade/get_portfolio')).rejects.toThrow('Invalid request')
  })

  it('throws McpError when response has no result field', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ jsonrpc: '2.0', id: 1 }), { status: 200 }),
    )

    await expect(mcpCall('/mcp/trading', 'trade/get_portfolio')).rejects.toThrow('no result field')
  })

  it('throws McpError on HTTP error', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('', { status: 500, statusText: 'Internal Server Error' }),
    )

    await expect(mcpCall('/mcp/trading', 'trade/get_portfolio')).rejects.toThrow('HTTP 500')
  })

  it('throws McpError when tool returns isError', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          jsonrpc: '2.0',
          id: 1,
          result: {
            content: [{ type: 'text', text: 'position limit exceeded' }],
            isError: true,
          },
        }),
        { status: 200 },
      ),
    )

    await expect(mcpCall('/mcp/trading', 'trade/submit_order')).rejects.toThrow('position limit exceeded')
  })

  it('throws McpError when tool returns no text content', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          jsonrpc: '2.0',
          id: 1,
          result: { content: [] },
        }),
        { status: 200 },
      ),
    )

    await expect(mcpCall('/mcp/trading', 'trade/get_portfolio')).rejects.toThrow('no text content')
  })
})

describe('McpError', () => {
  it('has code and data properties', () => {
    const err = new McpError(-32600, 'bad', { detail: 'x' })
    expect(err.code).toBe(-32600)
    expect(err.message).toBe('bad')
    expect(err.data).toEqual({ detail: 'x' })
    expect(err.name).toBe('McpError')
  })
})
