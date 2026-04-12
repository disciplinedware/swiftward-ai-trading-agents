require 'webmock/rspec'

class McpStubHelper
  include WebMock::API
  attr_reader :last_payload

  def initialize(url)
    @url = url # This can be a regex or base string
    @answers = {} # tool_name => response_body
    setup_stub
  end

  def set_answer(tool_name, response_body)
    @answers[tool_name.to_s] = response_body
  end

  private

  def setup_stub
    # Match any URL starting with the base @url
    stub_request(:post, /#{Regexp.escape(@url)}/).to_return(lambda { |request|
      @last_payload = JSON.parse(request.body)
      method = @last_payload["method"]

      if method == "initialize"
        # Optional: Validate headers if needed
        # target_file = request.headers["Mcp-Target-File"]

        {
          status: 200,
          body: {
            jsonrpc: "2.0",
            id: @last_payload["id"],
            result: { protocolVersion: "2024-11-05" }
          }.to_json,
          headers: {
            'Content-Type' => 'application/json',
            'Mcp-Session-Id' => 'test-session-id'
          }
        }
      elsif method == "tools/call"
        tool_name = @last_payload.dig("params", "name")
        if (answer = @answers[tool_name])
          {
            status: 200,
            body: {
              jsonrpc: "2.0",
              id: @last_payload["id"],
              result: answer
            }.to_json,
            headers: { 'Content-Type' => 'application/json' }
          }
        else
          {
            status: 404,
            body: { error: "No mock provided for tool: #{tool_name}" }.to_json,
            headers: { 'Content-Type' => 'application/json' }
          }
        end
      else
        # Handle tools/list if needed
        {
          status: 200,
          body: { jsonrpc: "2.0", id: @last_payload["id"], result: { tools: [] } }.to_json,
          headers: { 'Content-Type' => 'application/json' }
        }
      end
    })
  end
end
