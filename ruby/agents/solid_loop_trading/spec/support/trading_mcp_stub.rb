class TradingMcpStub
  def self.stub_all(url = "http://trading-mcp.local/mcp")
    WebMock.stub_request(:post, url).to_return(lambda do |request|
      # puts "[Stub] Request: #{request.body.to_s.truncate(100)}"
      
      env = Rack::MockRequest.env_for(url, 
        method: :post, 
        input: request.body, 
        "CONTENT_TYPE" => "application/json"
      )
      
      if request.headers["Mcp-Session-Id"]
        env["HTTP_MCP_SESSION_ID"] = request.headers["Mcp-Session-Id"]
      end

      status, headers, response_body = McpController.action(:call).call(env)
      
      body_text = ""
      response_body.each { |part| body_text << part }

      # puts "[Stub] Response: #{body_text.truncate(100)}"

      {
        status: status,
        headers: headers,
        body: body_text
      }
    end)
  end
end
