class BenchmarkWarmUpJob < ApplicationJob
  include ActionView::RecordIdentifier
  queue_as :default

  def perform(benchmark_id)
    benchmark = AgentBatch.find(benchmark_id)
    return unless benchmark

    benchmark.update!(warm_up_status: :executing, warm_up_response: nil)
    broadcast_status(benchmark)

    model_config = benchmark.agent_model
    provider = model_config.provider
    url = provider.base_url.to_s.gsub(/\/+$/, "") + "/v1/chat/completions"
    headers = {
      "Content-Type" => "application/json",
      "Authorization" => "Bearer #{provider.api_key}"
    }

    messages = [ { role: "user", content: "hello, write poem on ruby language" } ]

    begin
      conn = Faraday.new(url: url) do |f|
        f.options.timeout = 1800
        f.options.open_timeout = 10
        f.adapter Faraday.default_adapter
      end

      response = conn.post do |req|
        req.headers = headers
        req.body = { model: model_config.llm_model_name, messages: messages, max_tokens: 100 }.to_json
      end

      # OpenAI o-series rejects max_tokens — retry with max_completion_tokens
      if response.status == 400 && response.body.include?("max_tokens")
        response = conn.post do |req|
          req.headers = headers
          req.body = { model: model_config.llm_model_name, messages: messages, max_completion_tokens: 100 }.to_json
        end
      end

      if response.success?
        data = JSON.parse(response.body)
        choice = data.dig("choices", 0, "message") || {}
        content = choice["content"] || "No content"
        reasoning_content = choice["reasoning_content"] || choice["reasoning"] || choice["thought"]
        content = "(#{reasoning_content}) #{content}" if reasoning_content.present?

        benchmark.update!(
          warm_up_status: :warmed_up,
          warm_up_response: content,
          warm_up_at: Time.current
        )
      else
        error_msg = "HTTP #{response.status}: #{response.body}"
        benchmark.update!(
          warm_up_status: :not_started, # Reset so it can be retried
          warm_up_response: "Error: #{error_msg}",
          warm_up_at: Time.current
        )
      end
    rescue StandardError => e
      benchmark.update!(
        warm_up_status: :not_started,
        warm_up_response: "Exception: #{e.message}",
        warm_up_at: Time.current
      )
    end
    broadcast_status(benchmark)
  end

  private

  def broadcast_status(benchmark)
    Turbo::StreamsChannel.broadcast_update_to(
      benchmark,
      "agent_batch_warm_up_status",
      target: dom_id(benchmark, :warm_up_status),
      partial: "agent_batches/warm_up_status",
      locals: { benchmark: benchmark }
    )
    Turbo::StreamsChannel.broadcast_update_to(
      benchmark,
      "agent_batch_warm_up_status",
      target: dom_id(benchmark, :onboarding_alert),
      partial: "agent_batches/onboarding_alert",
      locals: { batch: benchmark }
    )
  end
end
