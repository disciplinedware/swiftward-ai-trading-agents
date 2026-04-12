module SolidLoop
  module Middlewares
    # Encodes "/" in MCP tool names to "__" before sending to LLM.
    # OpenAI enforces ^[a-zA-Z0-9_-]+$ for function names.
    # Pair with ToolNameDecoder inserted after NetworkCalling.
    class ToolNameEncoder
      def initialize(app)
        @app = app
      end

      def call(env)
        if env.payload
          encode_tools!(env.payload)
          encode_messages!(env.payload)
        end
        @app.call(env)
      end

      private

      def encode(name)
        name.to_s.gsub("/", "__")
      end

      def encode_tools!(payload)
        return unless payload[:tools]
        payload[:tools] = payload[:tools].map do |tool|
          next tool unless tool.dig(:function, :name)
          tool.merge(function: tool[:function].merge(name: encode(tool[:function][:name])))
        end
      end

      def encode_messages!(payload)
        return unless payload[:messages]
        payload[:messages] = payload[:messages].map do |msg|
          msg = msg.dup
          if msg[:tool_calls]
            msg[:tool_calls] = msg[:tool_calls].map do |tc|
              next tc unless tc.dig(:function, :name)
              tc.merge(function: tc[:function].merge(name: encode(tc.dig(:function, :name))))
            end
          end
          msg[:name] = encode(msg[:name]) if msg[:name]
          msg
        end
      end
    end
  end
end
