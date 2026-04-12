// JSON-RPC 2.0 client for MCP tool calls (tools/call method).

let nextId = 1

export interface JsonRpcRequest {
  jsonrpc: '2.0'
  id: number
  method: string
  params: Record<string, unknown>
}

export interface JsonRpcResponse<T = unknown> {
  jsonrpc: '2.0'
  id: number
  result?: T
  error?: {
    code: number
    message: string
    data?: unknown
  }
}

interface ToolResultContent {
  type: string
  text?: string
}

interface ToolResult {
  content: ToolResultContent[]
  isError?: boolean
}

export class McpError extends Error {
  code: number
  data?: unknown

  constructor(code: number, message: string, data?: unknown) {
    super(message)
    this.name = 'McpError'
    this.code = code
    this.data = data
  }
}

export function buildJsonRpcRequest(method: string, params: Record<string, unknown> = {}): JsonRpcRequest {
  return {
    jsonrpc: '2.0',
    id: nextId++,
    method,
    params,
  }
}

export function resetRequestId(): void {
  nextId = 1
}

/**
 * Call an MCP tool via JSON-RPC tools/call and unwrap the ToolResult.
 *
 * Sends: { method: "tools/call", params: { name: toolName, arguments: params } }
 * Receives: { result: { content: [{ type: "text", text: "<json>" }] } }
 * Returns: parsed JSON from the first text content block.
 */
export async function mcpCall<T>(
  endpoint: string,
  toolName: string,
  params: Record<string, unknown> = {},
  agentId?: string,
): Promise<T> {
  const body = buildJsonRpcRequest('tools/call', {
    name: toolName,
    arguments: params,
  })

  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  if (agentId) {
    headers['X-Agent-ID'] = agentId
  }

  const res = await fetch(endpoint, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })

  if (!res.ok) {
    throw new McpError(-1, `HTTP ${res.status}: ${res.statusText}`)
  }

  const json = (await res.json()) as JsonRpcResponse<ToolResult>

  if (json.error) {
    throw new McpError(json.error.code, json.error.message, json.error.data)
  }

  if (json.result === undefined) {
    throw new McpError(-1, 'Server returned success response with no result field')
  }

  const toolResult = json.result

  if (toolResult.isError) {
    const errorText = toolResult.content?.[0]?.text ?? 'Tool call failed'
    throw new McpError(-32000, errorText)
  }

  const textContent = toolResult.content?.find(c => c.type === 'text')
  if (!textContent?.text) {
    throw new McpError(-1, 'Tool returned no text content')
  }

  return JSON.parse(textContent.text) as T
}
