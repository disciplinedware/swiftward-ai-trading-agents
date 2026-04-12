Rails.application.config.to_prepare do
  # Monkey-patch: if base_url already has a version segment (/v2, /v4, etc.)
  # don't prepend /v1 — just append /chat/completions if not already present.
  SolidLoop::Dialects::OpenAi.prepend(Module.new do
    def completion_url(base_url)
      url = base_url.to_s.gsub(/\/+$/, "")
      return url if url.end_with?("/chat/completions")
      return url + "/chat/completions" if url.match?(/\/v\d+/)
      url + "/v1/chat/completions"
    end
  end)

  SolidLoop.configure do |config|
    config.llm_middlewares.delete(SolidLoop::Middlewares::ToolCallXmlParser)
    config.llm_middlewares.insert_before(SolidLoop::Middlewares::ResponseParsing, SolidLoop::Middlewares::ToolCallXmlParser)

    # Encode "/" in tool names to "__" before sending to LLM (OpenAI requires ^[a-zA-Z0-9_-]+$)
    # Decode back to "/" before ResponseParsing saves tool_call names to DB
    config.llm_middlewares.insert_before(SolidLoop::Middlewares::NetworkCalling, SolidLoop::Middlewares::ToolNameEncoder)
    config.llm_middlewares.insert_after(SolidLoop::Middlewares::NetworkCalling, SolidLoop::Middlewares::ToolNameDecoder)
  end
end
