require 'rails_helper'

RSpec.describe SolidLoop::SseStreamAggregator do
  let(:aggregator) { described_class.new }

  describe '#ingest' do
    it 'merges simple deltas into message' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello \"}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"}}]}\n\n")

      message = aggregator.result.dig("choices", 0, "message")
      expect(message["role"]).to eq("assistant")
      expect(message["content"]).to eq("Hello world")
    end

    it 'merges reasoning fields' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"Thinking...\"}}]}\n\n")

      message = aggregator.result.dig("choices", 0, "message")
      expect(message["reasoning_content"]).to eq("Thinking...")
    end

    it 'merges tool_calls by index' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\"}}]}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"q\\\"\"}}]}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":\\\"ruby\\\"}\"}}]}}]}\n\n")

      tool_calls = aggregator.result.dig("choices", 0, "message", "tool_calls")
      expect(tool_calls.size).to eq(1)
      expect(tool_calls[0]["id"]).to eq("call_1")
      expect(tool_calls[0]["function"]["name"]).to eq("search")
      expect(tool_calls[0]["function"]["arguments"]).to eq("{\"q\":\"ruby\"}")
    end

    it 'handles multiple tool_calls' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"id0\",\"function\":{\"name\":\"t0\"}}]}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"id1\",\"function\":{\"name\":\"t1\"}}]}}]}\n\n")

      tool_calls = aggregator.result.dig("choices", 0, "message", "tool_calls")
      expect(tool_calls.map { |tc| tc["function"]["name"] }).to eq([ "t0", "t1" ])
    end

    it 'handles usage and finish_reason' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}],\"usage\":{\"total_tokens\":10}}\n\n")

      expect(aggregator.result["usage"]["total_tokens"]).to eq(10)
      expect(aggregator.done?).to be true
    end

    it 'handles [DONE] signal' do
      aggregator.ingest("data: [DONE]\n\n")
      expect(aggregator.done?).to be true
    end

    it 'merges emulator-style chunks' do
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Let me \"}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"check the files first.\"}}]}\n\n")
      aggregator.ingest("data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_list_files\",\"function\":{\"name\":\"list_files\",\"arguments\":\"{}\"}}]}}]}\n\n")

      message = aggregator.result.dig("choices", 0, "message")
      expect(message["content"]).to eq("Let me check the files first.")
      expect(message["tool_calls"]).not_to be_empty
      expect(message["tool_calls"][0]["id"]).to eq("call_list_files")
    end

    it 'overwrites non-whitelisted string fields (metadata)' do
      aggregator.ingest("data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A\"}}]}\n\n")
      aggregator.ingest("data: {\"model\":\"gpt-4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"B\"}}]}\n\n")

      expect(aggregator.result["model"]).to eq("gpt-4") # NOT "gpt-4gpt-4"
      expect(aggregator.result.dig("choices", 0, "message", "content")).to eq("AB")
    end

    it 'correctly parses the stream_response.txt fixture' do
      fixture = File.read(Rails.root.join('spec', 'fixtures', 'stream_response.txt'))
      aggregator.ingest(fixture)

      message = aggregator.result.dig("choices", 0, "message")
      expect(message["reasoning"]).to include("I can see the CSV")
      expect(message["reasoning"]).to include("Let me create")
      expect(message["tool_calls"][0]["function"]["name"]).to eq("shell")
    end
  end
end
