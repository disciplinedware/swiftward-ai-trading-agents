require 'webmock/rspec'

class StreamingLlmEmulator
  include WebMock::API
  attr_reader :last_payload, :requests

  def initialize(base_url, api_token)
    @base_url = base_url.chomp('/')
    @api_token = api_token
    @answers = []
    @requests = []
    setup_stub
  end

  # answer_data is a hash representing the delta choice, e.g.:
  # { role: "assistant", content: "Hello", tool_calls: [...] }
  def set_answer(pattern, answer_data)
    @answers << { pattern: pattern, data: answer_data }
  end

  private

  def setup_stub
    stub_request(:post, "#{@base_url}/v1/chat/completions")
      .with(headers: { 'Authorization' => "Bearer #{@api_token}" })
      .to_return(lambda { |request|
        @last_payload = JSON.parse(request.body)
        @requests << @last_payload

        last_message = @last_payload["messages"]&.last
        content = last_message&.dig("content") || ""

        match_index = @answers.find_index { |a| content.match?(a[:pattern]) }
        match = @answers.delete_at(match_index) if match_index

        if match
          if @last_payload["stream"]
            # We format it exactly how the streaming parser expects:
            # A data prefix, stringified JSON, and two newlines
            choices = if match[:data][:choices]
              match[:data][:choices]
            else
              [
                {
                  index: 0,
                  delta: match[:data].except(:choices),
                  finish_reason: match[:data][:tool_calls] ? "tool_calls" : "stop"
                }
              ]
            end

            chunk_data = {
              id: "chatcmpl-#{SecureRandom.hex(4)}",
              object: "chat.completion.chunk",
              created: Time.now.to_i,
              model: @last_payload["model"],
              choices: choices,
              usage: {
                prompt_tokens: 10,
                completion_tokens: 10,
                total_tokens: 20,
                cost: 0.01
              }
            }.to_json

            stream_body = "data: #{chunk_data}\n\ndata: [DONE]\n\n"

            {
              status: 200,
              body: stream_body,
              headers: {
                'Content-Type' => 'text/event-stream',
                'Transfer-Encoding' => 'chunked'
              }
            }
          else
            choices = if match[:data][:choices]
              match[:data][:choices]
            else
              [
                {
                  index: 0,
                  message: match[:data].except(:choices),
                  finish_reason: match[:data][:tool_calls] ? "tool_calls" : "stop"
                }
              ]
            end

            json_response = {
              id: "chatcmpl-#{SecureRandom.hex(4)}",
              object: "chat.completion",
              created: Time.now.to_i,
              model: @last_payload["model"],
              choices: choices,
              usage: {
                prompt_tokens: 10,
                completion_tokens: 10,
                total_tokens: 20,
                cost: 0.01
              }
            }.to_json

            {
              status: 200,
              body: json_response,
              headers: {
                'Content-Type' => 'application/json'
              }
            }
          end
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
