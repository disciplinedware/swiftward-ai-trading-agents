require 'webmock/rspec'

class McpEmulator
  include WebMock::API

  attr_reader :requests, :initialized_options

  def initialize(url, expected_filename:)
    @url = url.chomp('/')
    @expected_filename = expected_filename
    @tools = load_tools_fixture
    @tool_results = setup_default_tool_results
    @requests = []
    @session_id = "mcp-session-#{SecureRandom.hex(4)}"
    setup_stub
  end

  def add_tool(name, return_value)
    # Simple tool registration
    @tools << { name: name, description: "Emulated tool #{name}", inputSchema: { type: "object", properties: {} } }
    @tool_results[name] = return_value
  end

  private

  def setup_default_tool_results
    {
      "list_files" => "candles.csv",
      "get_file_info" => "File: candles.csv\nSize: 42 bytes\nMIME: text/csv\nPreview:\ntimestamp,open,high,low,close,volume\n1712534400,65100.0,65300.0,65050.0,65200.0,120.5"
    }
  end

  def load_tools_fixture
    fixture_path = Rails.root.join('spec', 'fixtures', 'mcp_server_tools.json')
    if File.exist?(fixture_path)
      # The fixture contains the full response with { id: 2, result: { tools: [...] }, jsonrpc: "2.0" }
      data = JSON.parse(File.read(fixture_path))
      data.dig('result', 'tools') || []
    else
      []
    end
  end

  def setup_stub
    stub_request(:post, /#{Regexp.escape(@url)}/).to_return(lambda { |request|
      payload = JSON.parse(request.body)
      @requests << payload
      method = payload["method"]

      headers = { 'Content-Type' => 'application/json' }

      if method == "initialize"
        # Extract init options from the payload as the SolidLoop MCP client sends them
        options = payload.dig("params", "initializationOptions") || {}
        @initialized_options = options

        # SolidLoop sends it in options.
        received_filename = options["filename"]

        if @expected_filename && received_filename != @expected_filename
          return {
            status: 400,
            body: { error: "Expected filename '#{@expected_filename}', got '#{received_filename}'" }.to_json,
            headers: headers
          }
        end

        headers['Mcp-Session-Id'] = @session_id
        {
          status: 200,
          body: {
            jsonrpc: "2.0",
            id: payload["id"],
            result: { protocolVersion: "2024-11-05" }
          }.to_json,
          headers: headers
        }
      elsif method == "tools/list"
        if request.headers['Mcp-Session-Id'] != @session_id
          return {
            status: 400,
            body: { error: "Filename is required for new sessions (Missing or invalid Mcp-Session-Id header)" }.to_json,
            headers: headers
          }
        end

        {
          status: 200,
          body: {
            jsonrpc: "2.0",
            id: payload["id"],
            result: { tools: @tools }
          }.to_json,
          headers: headers
        }
      elsif method == "tools/call"
        if request.headers['Mcp-Session-Id'] != @session_id
          return {
            status: 400,
            body: { error: "Missing or invalid Mcp-Session-Id header" }.to_json,
            headers: headers
          }
        end

        tool_name = payload.dig("params", "name")
        if (result = @tool_results[tool_name])
          {
            status: 200,
            body: {
              jsonrpc: "2.0",
              id: payload["id"],
              result: {
                content: [
                  { type: "text", text: result.is_a?(String) ? result : result.to_json }
                ]
              }
            }.to_json,
            headers: headers
          }
        else
          {
            status: 404,
            body: { error: "Unknown tool: #{tool_name}" }.to_json,
            headers: headers
          }
        end
      else
        {
          status: 400,
          body: { error: "Unsupported method: #{method}" }.to_json,
          headers: headers
        }
      end
    })
  end
end
