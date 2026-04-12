require 'webmock/rspec'

class LlmStubHelper
  include WebMock::API
  attr_reader :last_payload

  def initialize(base_url, api_token)
    @base_url = base_url
    @api_token = api_token
    @answers = []
    @last_payload = nil
    setup_stub
  end

  def set_answer(pattern, json_response)
    @answers << { pattern: pattern, response: json_response }
  end

  private

  def setup_stub
    stub_request(:post, "#{@base_url}/v1/chat/completions")
      .with(headers: { 'Authorization' => "Bearer #{@api_token}" })
      .to_return(lambda { |request|
        @last_payload = JSON.parse(request.body)
        last_message = @last_payload["messages"]&.last
        content = last_message&.dig("content") || ""

        match = @answers.find { |a| content.match?(a[:pattern]) }

        if match
          {
            status: 200,
            body: match[:response].to_json,
            headers: { 'Content-Type' => 'application/json' }
          }
        else
          {
            status: 404,
            body: { error: "No matching pattern found for content: #{content}" }.to_json,
            headers: { 'Content-Type' => 'application/json' }
          }
        end
      })
  end
end
