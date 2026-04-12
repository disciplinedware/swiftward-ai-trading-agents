module SolidLoop
  module Middlewares
    # Decodes "__" back to "/" in tool call names from LLM response,
    # before ResponseParsing saves them to the database.
    class ToolNameDecoder
      def initialize(app)
        @app = app
      end

      def call(env)
        decode_normalized_data!(env)
        @app.call(env)
      end

      private

      def decode(name)
        name.to_s.gsub("__", "/")
      end

      def decode_normalized_data!(env)
        return unless env.normalized_data&.dig(:tool_calls)
        env.normalized_data[:tool_calls].each do |tc|
          next unless tc.dig("function", "name")
          tc["function"]["name"] = decode(tc["function"]["name"])
        end
      end
    end
  end
end
